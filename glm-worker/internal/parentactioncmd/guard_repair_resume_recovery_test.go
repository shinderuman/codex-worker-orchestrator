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
