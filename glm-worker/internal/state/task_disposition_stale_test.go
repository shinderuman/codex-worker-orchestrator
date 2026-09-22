package state

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestResetRequestRejectsLegacyOrphanWithoutDurableDispositionEvenWithStatsArchive(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	st.ArchiveCurrentStats()
	if _, err := os.Stat(st.TaskStatsArchivePath(taskID)); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("task.id", "task.status"); err != nil {
		t.Fatal(err)
	}

	if err := st.ValidateResetRequest(string(TaskDispositionAbandon)); err == nil || !strings.Contains(err.Error(), "no durable reset disposition") {
		t.Fatalf("legacy orphan was recovered from observational TaskStats: %v", err)
	}
	if _, err := st.ResetWithDisposition(string(TaskDispositionAbandon)); err == nil || !strings.Contains(err.Error(), "no durable reset disposition") {
		t.Fatalf("legacy orphan reset used observational TaskStats: %v", err)
	}
	if _, err := st.CurrentTaskDisposition(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected orphan reset created disposition: %v", err)
	}
}

func TestResetRequestRejectsOrphanedTaskIdentityWithoutDurableDisposition(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	otherTaskID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.writeParentReviewState(ParentReviewState{Version: parentReviewStateVersion, TaskID: otherTaskID}); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("task.id"); err != nil {
		t.Fatal(err)
	}

	err = st.ValidateResetRequest(string(TaskDispositionAbandon))
	if err == nil || !strings.Contains(err.Error(), "no durable reset disposition") {
		t.Fatalf("orphaned task identity without canonical disposition was accepted: %v", err)
	}
}

func TestResetParentReviewUsesCanonicalStructuralDecoder(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	state := ParentReviewState{
		Version: parentReviewStateVersion,
		TaskID:  taskID,
		Open:    &ParentReviewOpenState{PacketStatus: "invalid"},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(parentReviewStateFile), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Remove("task.id"); err != nil {
		t.Fatal(err)
	}

	_, err = st.rawParentReviewStateForReset()
	if err == nil || !strings.Contains(err.Error(), "parent review stateのpacket statusが不正です") {
		t.Fatalf("reset reader bypassed canonical parent-review decoder: %v", err)
	}
}

func TestResetRequestRejectsOrphanedCompleteWithPendingPassWithoutDurableDisposition(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}

	st.ArchiveCurrentStats()
	if err := st.Remove("task.id", "task.status"); err != nil {
		t.Fatal(err)
	}

	if err := st.ValidateResetRequest(""); err == nil || !strings.Contains(err.Error(), "no durable reset disposition") {
		t.Fatalf("orphaned complete task accepted generic reset: %v", err)
	}
	if err := st.ValidateResetRequest(string(TaskDispositionAbandon)); err == nil || !strings.Contains(err.Error(), "no durable reset disposition") {
		t.Fatalf("orphaned complete task was reconstructed from TaskStats: %v", err)
	}
}

func TestResetDispositionIgnoresCorruptCurrentTaskStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(currentStatsFile), []byte(`{"version":999,"schema_revision":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreWarnings := RedirectStatsWarnings(io.Discard)
	defer restoreWarnings()

	if err := st.ValidateResetRequest(string(TaskDispositionAbandon)); err != nil {
		t.Fatalf("corrupt observational TaskStats blocked reset admission: %v", err)
	}
	if _, err := st.ResetWithDisposition(string(TaskDispositionAbandon)); err != nil {
		t.Fatalf("corrupt observational TaskStats blocked reset transition: %v", err)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("corrupt observational TaskStats blocked new-task admission: %v", err)
	}
}
