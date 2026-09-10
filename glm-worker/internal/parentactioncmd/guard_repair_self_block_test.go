package parentactioncmd

import (
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRequestSelfBlockedGuardRepairRecordsSameGuardFamily(t *testing.T) {
	cfg, st, _ := newGuardRepairLifecycleState(t)
	checkpoint := selfBlockedGuardCheckpoint("git authority guard failed: after-call-mutation: refs changed")
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	failure := "git authority guard failed: capture-before-call: cannot enumerate protected refs"
	stderr := []byte("{\"error\":{\"kind\":\"worker_error\",\"message\":\"" + failure + "\"}}")

	if err := requestSelfBlockedGuardRepair(cfg, st, stderr); err != nil {
		t.Fatal(err)
	}
	record, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != state.GuardRepairRequested || record.TaskID != st.ReadOr("task.id", "") || record.Phase != checkpoint.Phase {
		t.Fatalf("guard repair record = %#v", record)
	}
	if record.Failure != failure {
		t.Fatalf("guard repair failure = %q", record.Failure)
	}
}

func TestRequestSelfBlockedGuardRepairIgnoresDifferentGuardFamily(t *testing.T) {
	cfg, st, _ := newGuardRepairLifecycleState(t)
	checkpoint := selfBlockedGuardCheckpoint("git authority guard failed: after-call-mutation: refs changed")
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	stderr := []byte("{\"error\":{\"kind\":\"worker_error\",\"message\":\"repository instruction surface guard failed: before-call-mismatch: changed\"}}")

	if err := requestSelfBlockedGuardRepair(cfg, st, stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadGuardRepairRecord(); !errors.Is(err, state.ErrNoGuardRepairRecord) {
		t.Fatalf("different guard family created repair record: %v", err)
	}
}

func TestRequestSelfBlockedGuardRepairIgnoresNonPreCallFailure(t *testing.T) {
	cfg, st, _ := newGuardRepairLifecycleState(t)
	checkpoint := selfBlockedGuardCheckpoint("git authority guard failed: after-call-mutation: refs changed")
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	stderr := []byte("{\"error\":{\"kind\":\"worker_error\",\"message\":\"git authority guard failed: after-call-mutation: refs changed\"}}")

	if err := requestSelfBlockedGuardRepair(cfg, st, stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadGuardRepairRecord(); !errors.Is(err, state.ErrNoGuardRepairRecord) {
		t.Fatalf("non-pre-call failure created repair record: %v", err)
	}
}

func selfBlockedGuardCheckpoint(failure string) state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:        state.ResumeStageWorker,
		Phase:        "worker-new",
		Role:         state.WorkerRole,
		Model:        "worker-model",
		Prompt:       "prompt",
		Request:      "request",
		StopKind:     state.ResumeStopGuardRecoverable,
		GuardFailure: failure,
	}
}
