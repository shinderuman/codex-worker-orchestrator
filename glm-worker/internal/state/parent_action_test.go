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

func TestParentActionPlanStoppedDecisionContinuationAdmitsResume(t *testing.T) {
	cases := []struct {
		name     string
		status   TaskStatus
		stopKind ResumeStopKind
		phase    string
	}{
		{name: "rate limited", status: TaskStatusRateLimited, stopKind: ResumeStopRateLimited, phase: "worker-decision"},
		{name: "rate limited result correction", status: TaskStatusRateLimited, stopKind: ResumeStopRateLimited, phase: "worker-decision-result-correct"},
		{name: "provider unavailable", status: TaskStatusProviderUnavailable, stopKind: ResumeStopProviderUnavailable, phase: "worker-decision"},
		{name: "interrupted", status: TaskStatusInterrupted, stopKind: ResumeStopInterrupted, phase: "worker-decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newParentActionTestStore(t)
			seedStoppedDecisionCheckpoint(t, st, WorkerRole, tc.phase, tc.stopKind, "decision-body")
			if err := st.SetTaskStatus(tc.status); err != nil {
				t.Fatal(err)
			}

			plan, err := st.ParentActionPlan()
			if err != nil {
				t.Fatal(err)
			}
			if plan.RequiredAction != ParentActionResume || plan.ResumeKind != string(tc.stopKind) ||
				!plan.Allows(ParentActionResume) || plan.Allows(ParentActionDecision) || plan.Allows(ParentActionFix) {
				t.Fatalf("stopped decision plan = %#v", plan)
			}
			if _, admitted, err := st.AdmitParentAction(ParentActionDecision); err != nil || admitted {
				t.Fatalf("decision reissue admission = admitted %v err %v", admitted, err)
			}
		})
	}
}

func TestParentActionPlanStoppedDecisionContinuationAdmitsPaddedDecisionPayload(t *testing.T) {
	st := newParentActionTestStore(t)
	seedStoppedDecisionCheckpoint(t, st, WorkerRole, "worker-decision", ResumeStopRateLimited, " decision-body \n")
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionResume || !plan.Allows(ParentActionResume) {
		t.Fatalf("padded decision continuation plan = %#v", plan)
	}
}

func TestParentActionPlanStoppedPendingDecisionFailsClosed(t *testing.T) {
	cases := []struct {
		name         string
		status       TaskStatus
		role         SessionRole
		phase        string
		stopKind     ResumeStopKind
		decision     string
		lastDecision string
		noCheckpoint bool
	}{
		{
			name:         "decision checkpoint missing",
			status:       TaskStatusRateLimited,
			role:         WorkerRole,
			phase:        "worker-decision",
			stopKind:     ResumeStopRateLimited,
			decision:     "decision-body",
			lastDecision: "decision-body",
			noCheckpoint: true,
		},
		{
			name:         "worker phase mismatch",
			status:       TaskStatusRateLimited,
			role:         WorkerRole,
			phase:        "worker-new",
			stopKind:     ResumeStopRateLimited,
			decision:     "decision-body",
			lastDecision: "decision-body",
		},
		{
			name:         "reviewer phase pending marker",
			status:       TaskStatusProviderUnavailable,
			role:         ReviewerRole,
			phase:        "reviewer-1",
			stopKind:     ResumeStopProviderUnavailable,
			decision:     "decision-body",
			lastDecision: "decision-body",
		},
		{
			name:     "missing decision body",
			status:   TaskStatusRateLimited,
			role:     WorkerRole,
			phase:    "worker-decision",
			stopKind: ResumeStopRateLimited,
		},
		{
			name:         "decision identity mismatch",
			status:       TaskStatusInterrupted,
			role:         WorkerRole,
			phase:        "worker-decision",
			stopKind:     ResumeStopInterrupted,
			decision:     "checkpoint-decision",
			lastDecision: "state-decision",
		},
		{
			name:         "whitespace only decision identity mismatch",
			status:       TaskStatusRateLimited,
			role:         WorkerRole,
			phase:        "worker-decision",
			stopKind:     ResumeStopRateLimited,
			decision:     " decision-body ",
			lastDecision: "decision-body",
		},
		{
			name:         "guard recoverable pending marker",
			status:       TaskStatusGuardRecoverable,
			role:         WorkerRole,
			phase:        "worker-decision",
			stopKind:     ResumeStopGuardRecoverable,
			decision:     "decision-body",
			lastDecision: "decision-body",
		},
		{
			name:         "stop kind status mismatch",
			status:       TaskStatusRateLimited,
			role:         WorkerRole,
			phase:        "worker-decision",
			stopKind:     ResumeStopInterrupted,
			decision:     "decision-body",
			lastDecision: "decision-body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newParentActionTestStore(t)
			if err := st.Touch("pending-decision"); err != nil {
				t.Fatal(err)
			}
			if !tc.noCheckpoint {
				seedStoppedDecisionCheckpoint(t, st, tc.role, tc.phase, tc.stopKind, tc.decision)
				if tc.lastDecision != "" {
					if err := st.Write("last-decision", tc.lastDecision); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := st.SetTaskStatus(tc.status); err != nil {
				t.Fatal(err)
			}

			if _, err := st.ParentActionPlan(); err == nil {
				t.Fatal("stopped pending decision state outside worker-decision continuation was accepted")
			}
		})
	}
}

func seedStoppedDecisionCheckpoint(t *testing.T, st *StateStore, role SessionRole, phase string, stopKind ResumeStopKind, decision string) {
	t.Helper()
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if decision != "" {
		if err := st.Write("last-decision", decision); err != nil {
			t.Fatal(err)
		}
	}
	checkpoint := ResumeCheckpoint{
		Stage:    stageForRole(role),
		Phase:    phase,
		Role:     role,
		Model:    "opus",
		Prompt:   "p",
		Request:  "r",
		Decision: decision,
		StopKind: stopKind,
	}
	switch stopKind {
	case ResumeStopRateLimited:
		checkpoint.ResetAtCST = "2026-09-05 03:32:23"
		checkpoint.ResetAtRFC3339 = "2026-09-05T03:32:23+08:00"
	case ResumeStopProviderUnavailable:
		checkpoint.ProviderUnavailableClassification = "http-503"
		checkpoint.ProviderUnavailableProbes = 4
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
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

func stageForRole(role SessionRole) ResumeStage {
	if role == ReviewerRole {
		return ResumeStageReview
	}
	return ResumeStageWorker
}
