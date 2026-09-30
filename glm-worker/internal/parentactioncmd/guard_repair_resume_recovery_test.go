package parentactioncmd

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecoverGuardRepairResumeResetsObservedStoppedAttemptToReady(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	persistReadyGuardRepair(t, cfg, st, &record)
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	attemptID := "44444444-4444-4444-8444-444444444444"
	if _, err := st.PrepareGuardRepairResume(record, checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if err := st.ObserveGuardRepairResume(checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	if err := recoverGuardRepairResumeIfNeeded(st); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairReady || got.ResumeAttemptID != "" || got.OriginalResumeObserved {
		t.Fatalf("stopped rebuilt resume did not return repair transaction to ready: %#v", got)
	}
}

func TestRecoverGuardRepairResumeReconcilesUnobservedActiveBoundary(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	persistReadyGuardRepair(t, cfg, st, &record)
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	attemptID := "77777777-7777-4777-8777-777777777777"
	if _, err := st.PrepareGuardRepairResume(record, checkpoint, attemptID); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginResume(checkpoint); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("crash fixture status = %s want active", st.TaskStatus())
	}

	if err := recoverGuardRepairResumeIfNeeded(st); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("recovered status = %s want guard-recoverable", st.TaskStatus())
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairReady || got.ResumeAttemptID != "" || got.ResumeCheckpointDigest != "" || got.OriginalResumeObserved {
		t.Fatalf("active-boundary startup recovery did not reset transaction: %#v", got)
	}
}
