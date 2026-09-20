package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func newResetDispositionArchiveAdmissionFixture(t *testing.T) (*StateStore, string) {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetWithDisposition(string(TaskDispositionAbandon)); err != nil {
		t.Fatal(err)
	}
	return st, taskID
}

func TestValidateResetDispositionForNewTaskRejectsMissingArchiveEvidence(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	if err := os.Remove(st.TaskStatsArchivePath(taskID)); err != nil {
		t.Fatal(err)
	}

	err := st.ValidateResetDispositionForNewTask()
	if err == nil || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "cannot verify archived reset task stats") {
		t.Fatalf("missing archive evidence did not fail closed: %v", err)
	}
}

func TestValidateResetDispositionForNewTaskRejectsUnprovenArchiveEvidence(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	otherTaskID, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(taskStatsArchiveIdentity{
		Version:        taskStatsVersion,
		SchemaRevision: taskStatsSchemaRevision,
		TaskID:         otherTaskID,
		Status:         TaskStatusAwaitingParentCompletion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskStatsArchivePath(taskID), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	err = st.ValidateResetDispositionForNewTask()
	if err == nil || !strings.Contains(err.Error(), "archive evidence is unproven") {
		t.Fatalf("unproven archive evidence did not fail closed: %v", err)
	}
}

func TestValidateResetDispositionForNewTaskRejectsArchiveStatusMismatch(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	path := st.TaskStatsArchivePath(taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stats TaskStats
	if err := json.Unmarshal(data, &stats); err != nil {
		t.Fatal(err)
	}
	stats.Status = TaskStatusInterrupted
	data, err = json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	err = st.ValidateResetDispositionForNewTask()
	if err == nil || !strings.Contains(err.Error(), "does not match archived task status") {
		t.Fatalf("archive status mismatch did not fail closed: %v", err)
	}
}
