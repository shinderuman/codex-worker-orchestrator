package app

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestResumeCommandAdmissionBindsRateLimitResetWindow(t *testing.T) {
	cases := []struct {
		name       string
		resetAt    time.Time
		wantDenied bool
	}{
		{name: "direct resume before the reset is rejected", resetAt: time.Now().Add(time.Hour), wantDenied: true},
		{name: "manual fallback after the reset is admitted", resetAt: time.Now().Add(-time.Minute)},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker", Role: state.WorkerRole, Model: "opus"}
			checkpoint.SetStopKind(state.ResumeStopRateLimited)
			checkpoint.ResetAtRFC3339 = c.resetAt.Format(time.RFC3339)
			if err := st.EnterStop(checkpoint); err != nil {
				t.Fatal(err)
			}

			err = admitParentCommand(Command{Mode: ModeResume}, st)
			if c.wantDenied {
				var workerErr *workflow.WorkerError
				if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "cannot resume before the Z.ai 5h reset at") {
					t.Fatalf("resume admission error = %v", err)
				}
				if status := st.TaskStatus(); status != state.TaskStatusRateLimited {
					t.Fatalf("rejected resume must keep the stopped task state, got %s", status)
				}
				return
			}
			if err != nil {
				t.Fatalf("resume after the reset was rejected: %v", err)
			}
		})
	}
}

func TestResumeCommandAdmissionFailsClosedWithoutResetEvidence(t *testing.T) {
	st := newParentAdmissionStore(t)
	checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker", Role: state.WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}

	err := admitParentCommand(Command{Mode: ModeResume}, st)
	var workerErr *workflow.WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "rate-limit reset evidence is missing") {
		t.Fatalf("missing reset admission error = %v", err)
	}
	if status := st.TaskStatus(); status != state.TaskStatusRateLimited {
		t.Fatalf("missing reset rejection must retain rate-limited state, got %s", status)
	}
}

func TestResumeCommandAdmissionFailsClosedForUnreadableResetEvidence(t *testing.T) {
	st := newParentAdmissionStore(t)
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	malformed := `{"version":6,"stage":"worker","phase":"worker","role":"worker","model":"opus","report_only":false,"stop_kind":"rate-limited","reset_at_rfc3339":"not-a-timestamp"}`
	if err := os.WriteFile(st.Path("resume-state.json"), []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}

	err := admitParentCommand(Command{Mode: ModeResume}, st)
	var workerErr *workflow.WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "resume checkpoint is unreadable: rate-limit reset_at_rfc3339 is invalid") {
		t.Fatalf("unreadable reset admission error = %v", err)
	}
	if status := st.TaskStatus(); status != state.TaskStatusRateLimited {
		t.Fatalf("unreadable reset rejection must retain rate-limited state, got %s", status)
	}
}

func TestParentCommandAdmissionMatchesWaitingActions(t *testing.T) {
	t.Run("decision", func(t *testing.T) {
		st := newParentAdmissionStore(t)
		if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			t.Fatal(err)
		}
		if err := st.Touch("pending-decision"); err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}

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
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}

		if err := admitParentCommand(Command{Mode: ModeFix}, st); err != nil {
			t.Fatalf("fix rejected during parent review: %v", err)
		}
		for _, mode := range []CommandMode{ModeAccept, ModeDecision, ModeResume, ModeNewTask} {
			if err := admitParentCommand(Command{Mode: mode}, st); err == nil {
				t.Fatalf("mode %d admitted during unbound parent review", mode)
			}
		}
	})
}

func TestParentCommandAdmissionPreservesPassAcceptance(t *testing.T) {
	st := newParentAdmissionStore(t)
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}

	if err := admitParentCommand(Command{Mode: ModeAccept}, st); err != nil {
		t.Fatalf("PASS accept rejected: %v", err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err == nil {
		t.Fatal("new task admitted before PASS acceptance")
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err == nil {
		t.Fatal("new task admitted while awaiting parent completion")
	}
	if err := admitParentCommand(Command{Mode: ModeAccept}, st); err == nil {
		t.Fatal("accept admitted while awaiting parent completion")
	}
	if _, err := st.CompleteParentAwaiting(nil); err != nil {
		t.Fatal(err)
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("new task rejected after parent completion: %v", err)
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
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Prompt:         "p",
		Request:        "r",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtRFC3339: time.Now().Add(-time.Minute).Format(time.RFC3339),
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
