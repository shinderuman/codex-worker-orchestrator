package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestOrdinaryParentNewTaskAdmissionDoesNotRequireSessionRotationClaim(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	seedParentAdmissionPendingRotation(t, st, parentThread)
	t.Setenv(state.ParentActionCodexThreadIDEnv, parentThread)
	t.Setenv(state.SessionRotationClaimIDEnv, "")

	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("ordinary parent new-task admission was controlled by an unclaimed rotation recommendation: %v", err)
	}
}

func seedParentAdmissionPendingRotation(t *testing.T, st *state.StateStore, parentThread string) {
	t.Helper()
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteParentAwaiting(func(string) (*state.SessionRotationEvaluation, error) {
		return &state.SessionRotationEvaluation{
			ParentThreadID: parentThread,
			TaskID:         st.ReadOr("task.id", ""),
			Terminal:       state.SessionRotationTerminalAccept,
			Decision: state.SessionRotationDecision{
				Required: true,
				Reason:   state.SessionRotationReasonCompaction,
			},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	projection, err := st.ProjectSessionRotation(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != state.SessionRotationProjectionPending || projection.Directive == nil {
		t.Fatalf("test did not create a pending rotation recommendation: %#v", projection)
	}
}
