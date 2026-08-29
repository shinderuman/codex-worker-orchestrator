package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentCommandAdmissionMatchesWaitingActions(t *testing.T) {
	t.Run("decision", func(t *testing.T) {
		st := newParentAdmissionStore(t)
		if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			t.Fatal(err)
		}
		if err := st.Touch("pending-decision"); err != nil {
			t.Fatal(err)
		}
		st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{})

		if err := admitParentCommand(Command{Mode: ModeDecision}, st); err != nil {
			t.Fatalf("decision rejected: %v", err)
		}
		for _, mode := range []CommandMode{ModeFix, ModeAccept, ModeResume, ModeNewTask} {
			if err := admitParentCommand(Command{Mode: mode}, st); err == nil {
				t.Fatalf("mode %d admitted during pending decision", mode)
			}
		}
	})

	t.Run("parent review", func(t *testing.T) {
		st := newParentAdmissionStore(t)
		if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
			t.Fatal(err)
		}
		st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{})

		for _, mode := range []CommandMode{ModeAccept, ModeFix} {
			if err := admitParentCommand(Command{Mode: mode}, st); err != nil {
				t.Fatalf("mode %d rejected during parent review: %v", mode, err)
			}
		}
		for _, mode := range []CommandMode{ModeDecision, ModeResume, ModeNewTask} {
			if err := admitParentCommand(Command{Mode: mode}, st); err == nil {
				t.Fatalf("mode %d admitted during parent review", mode)
			}
		}
	})
}

func TestParentCommandAdmissionPreservesPassAcceptance(t *testing.T) {
	st := newParentAdmissionStore(t)
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{})

	if err := admitParentCommand(Command{Mode: ModeAccept}, st); err != nil {
		t.Fatalf("PASS accept rejected: %v", err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err == nil {
		t.Fatal("new task admitted before PASS acceptance")
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("new task rejected after PASS acceptance: %v", err)
	}
}

func TestParentCommandAdmissionPreservesAcceptNoOpAndResetEscape(t *testing.T) {
	st := newParentAdmissionStore(t)
	if err := admitParentCommand(Command{Mode: ModeAccept}, st); err != nil {
		t.Fatalf("idempotent accept no-op was rejected: %v", err)
	}

	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := admitParentCommand(Command{Mode: ModeReset}, st); err != nil {
		t.Fatalf("reset escape hatch was blocked by inconsistent lifecycle: %v", err)
	}
	if err := admitParentCommand(Command{Mode: ModeDecision}, st); err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency") {
		t.Fatalf("contradictory decision state did not fail closed: %v", err)
	}
}

func TestParentCommandAdmissionStoppedTaskRequiresResume(t *testing.T) {
	st := newParentAdmissionStore(t)
	checkpoint := state.ResumeCheckpoint{
		Stage:       state.ResumeStageWorker,
		Phase:       "worker-new",
		Role:        state.WorkerRole,
		Model:       "opus",
		Prompt:      "p",
		Request:     "r",
		RateLimited: true,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if err := admitParentCommand(Command{Mode: ModeResume}, st); err != nil {
		t.Fatalf("resume rejected: %v", err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err == nil {
		t.Fatal("new task admitted during rate limit")
	}
	if err := admitParentCommand(Command{Mode: ModeAccept}, st); err != nil {
		t.Fatalf("existing idempotent accept no-op changed during rate limit: %v", err)
	}
}

func newParentAdmissionStore(t *testing.T) *state.StateStore {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}
