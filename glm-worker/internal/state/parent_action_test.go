package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentActionPlanWaitingStates(t *testing.T) {
	t.Run("decision", func(t *testing.T) {
		st := newParentActionTestStore(t)
		if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
			t.Fatal(err)
		}
		if err := st.Touch("pending-decision"); err != nil {
			t.Fatal(err)
		}
		st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{})

		plan, err := st.ParentActionPlan()
		if err != nil {
			t.Fatal(err)
		}
		if plan.RequiredAction != ParentActionDecision || !plan.Allows(ParentActionDecision) || plan.Allows(ParentActionFix) {
			t.Fatalf("decision plan = %#v", plan)
		}
	})

	t.Run("parent review", func(t *testing.T) {
		st := newParentActionTestStore(t)
		if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
			t.Fatal(err)
		}
		st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{})

		plan, err := st.ParentActionPlan()
		if err != nil {
			t.Fatal(err)
		}
		if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionAccept) || !plan.Allows(ParentActionFix) || plan.Allows(ParentActionDecision) {
			t.Fatalf("review plan = %#v", plan)
		}
	})
}

func TestParentActionPlanStoppedStates(t *testing.T) {
	cases := []struct {
		name     string
		status   TaskStatus
		stopKind ResumeStopKind
		required ParentAction
	}{
		{name: "rate limited", status: TaskStatusRateLimited, stopKind: ResumeStopRateLimited, required: ParentActionResume},
		{name: "provider unavailable", status: TaskStatusProviderUnavailable, stopKind: ResumeStopProviderUnavailable, required: ParentActionResume},
		{name: "interrupted", status: TaskStatusInterrupted, stopKind: ResumeStopInterrupted, required: ParentActionResume},
		{name: "guard recoverable", status: TaskStatusGuardRecoverable, stopKind: ResumeStopGuardRecoverable, required: ParentActionRepairGuardThenResume},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newParentActionTestStore(t)
			checkpoint := ResumeCheckpoint{
				Stage:    ResumeStageWorker,
				Phase:    "worker-new",
				Role:     WorkerRole,
				Model:    "opus",
				Prompt:   "p",
				Request:  "r",
				StopKind: tc.stopKind,
			}
			if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(tc.status); err != nil {
				t.Fatal(err)
			}

			plan, err := st.ParentActionPlan()
			if err != nil {
				t.Fatal(err)
			}
			if plan.RequiredAction != tc.required || plan.ResumeKind != string(tc.stopKind) || !plan.Allows(ParentActionResume) {
				t.Fatalf("stop plan = %#v", plan)
			}
		})
	}
}

func TestParentActionPlanPassRequiresAcceptUntilResolved(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{})

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionAccept || !plan.Allows(ParentActionAccept) || plan.Allows(ParentActionFix) {
		t.Fatalf("PASS plan = %#v", plan)
	}

	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("PASS accept = %v err=%v", accepted, err)
	}
	plan, err = st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionNone || len(plan.AllowedActions) != 0 {
		t.Fatalf("accepted complete plan = %#v", plan)
	}
}

func TestParentActionPlanPreservesIdempotentAcceptCommand(t *testing.T) {
	st := newParentActionTestStore(t)
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionNone || plan.Allows(ParentActionAccept) || !plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("no-action plan = %#v", plan)
	}
}

func TestParentActionPlanFailsClosedOnContradiction(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("waiting-decision without pending decision or parent review was accepted")
	}

	st = newParentActionTestStore(t)
	checkpoint := ResumeCheckpoint{
		Stage:    ResumeStageWorker,
		Phase:    "worker-new",
		Role:     WorkerRole,
		Model:    "opus",
		Prompt:   "p",
		Request:  "r",
		StopKind: ResumeStopInterrupted,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("checkpoint stop kind inconsistent with task status was accepted")
	}
}

func TestParentActionPlanCompleteNeedsNoAction(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionNone || len(plan.AllowedActions) != 0 {
		t.Fatalf("complete plan = %#v", plan)
	}
}

func newParentActionTestStore(t *testing.T) *StateStore {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}
