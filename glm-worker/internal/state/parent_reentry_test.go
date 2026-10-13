package state

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func newControllerReentryStore(t *testing.T, completionRisk packet.Risk) *StateStore {
	t.Helper()
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: completionRisk}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	if err := st.SaveControllerRuntimeBinding(ControllerRuntimeBinding{
		AttemptID:          "accepted-attempt",
		TaskPath:           "IMPLEMENTATION_TASKS/active.md",
		TaskContractDigest: "contract-digest",
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func controllerReentryAuthority() ControllerReentryAuthority {
	return ControllerReentryAuthority{
		PredecessorAttemptID: "accepted-attempt",
		AttemptID:            "reentered-attempt",
		TaskPath:             "IMPLEMENTATION_TASKS/active.md",
		TaskContractDigest:   "contract-digest",
	}
}

func TestReenterAcceptedCompletionFromControllerReturnsTaskToFixLifecycle(t *testing.T) {
	st := newControllerReentryStore(t, packet.RiskHigh)
	reentered, err := st.ReenterAcceptedCompletionFromController(controllerReentryAuthority())
	if err != nil || !reentered {
		t.Fatalf("controller reentry = %v err=%v", reentered, err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("reentered status = %s", st.TaskStatus())
	}
	open, err := st.CurrentParentReview()
	if err != nil || open == nil || open.PacketStatus != string(packet.StatusNeedsSolReview) || open.Risk != string(packet.RiskHigh) {
		t.Fatalf("reentered review = %#v err=%v", open, err)
	}
	if _, err := st.CurrentParentCompletionOutcome(); err != nil {
		t.Fatal(err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionFix) || plan.Allows(ParentActionComplete) || plan.Allows(ParentActionInstall) {
		t.Fatalf("reentered plan = %#v", plan)
	}
	binding, err := st.LoadControllerRuntimeBinding()
	if err != nil || binding.AttemptID != "accepted-attempt" {
		t.Fatalf("reentry consumed the workflow binding before the next model run: %#v %v", binding, err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeFix] != 1 || stats.ParentOutcomes[ParentOutcomeAccepted] != 1 {
		t.Fatalf("reentry outcomes = %#v", stats.ParentOutcomes)
	}
	if stats.ParentFixOrigins[ParentOriginUserAmendment] != 1 {
		t.Fatalf("reentry fix origin evidence = %#v", stats.ParentFixOrigins)
	}
}

func TestReenterAcceptedCompletionFromControllerRaisesLowRiskCompletionToFloor(t *testing.T) {
	st := newControllerReentryStore(t, packet.RiskLow)
	if _, err := st.ReenterAcceptedCompletionFromController(controllerReentryAuthority()); err != nil {
		t.Fatal(err)
	}
	open, err := st.CurrentParentReview()
	if err != nil || open == nil || open.Risk != string(packet.RiskHigh) {
		t.Fatalf("reentered low-risk review = %#v err=%v", open, err)
	}
}

func TestReenterAcceptedCompletionFromControllerFailsClosed(t *testing.T) {
	cases := []struct {
		name      string
		authority func() ControllerReentryAuthority
		expected  string
	}{
		{
			name: "foreign predecessor",
			authority: func() ControllerReentryAuthority {
				authority := controllerReentryAuthority()
				authority.PredecessorAttemptID = "unrelated-attempt"
				return authority
			},
			expected: "canonical attempt predecessor",
		},
		{
			name: "same attempt without reentry transition",
			authority: func() ControllerReentryAuthority {
				authority := controllerReentryAuthority()
				authority.AttemptID = "accepted-attempt"
				return authority
			},
			expected: "canonical attempt predecessor",
		},
		{
			name: "missing reentered attempt identity",
			authority: func() ControllerReentryAuthority {
				authority := controllerReentryAuthority()
				authority.AttemptID = ""
				return authority
			},
			expected: "canonical attempt predecessor",
		},
		{
			name: "task path mismatch",
			authority: func() ControllerReentryAuthority {
				authority := controllerReentryAuthority()
				authority.TaskPath = "IMPLEMENTATION_TASKS/successor.md"
				return authority
			},
			expected: "workflow binding",
		},
		{
			name: "task contract mismatch",
			authority: func() ControllerReentryAuthority {
				authority := controllerReentryAuthority()
				authority.TaskContractDigest = "stale-digest"
				return authority
			},
			expected: "workflow binding",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newControllerReentryStore(t, packet.RiskHigh)
			reentered, err := st.ReenterAcceptedCompletionFromController(tc.authority())
			if err == nil || reentered || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("reentry = %v err=%v want %q", reentered, err, tc.expected)
			}
			if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
				t.Fatalf("failed reentry consumed the awaiting state: %s", st.TaskStatus())
			}
		})
	}
}

func TestReenterAcceptedCompletionWithoutOutcomeFailsClosed(t *testing.T) {
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveControllerRuntimeBinding(ControllerRuntimeBinding{
		AttemptID:          "accepted-attempt",
		TaskPath:           "IMPLEMENTATION_TASKS/active.md",
		TaskContractDigest: "contract-digest",
	}); err != nil {
		t.Fatal(err)
	}
	reentered, err := st.ReenterAcceptedCompletionFromController(controllerReentryAuthority())
	if err == nil || reentered || !strings.Contains(err.Error(), "accepted parent completion outcome") {
		t.Fatalf("reentry = %v err=%v", reentered, err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("failed reentry consumed the awaiting state: %s", st.TaskStatus())
	}
}

func TestReenterAcceptedCompletionFromControllerSkipsNonAwaitingRuntime(t *testing.T) {
	st := newControllerReentryStore(t, packet.RiskHigh)
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	reentered, err := st.ReenterAcceptedCompletionFromController(controllerReentryAuthority())
	if err != nil || reentered {
		t.Fatalf("non-awaiting reentry = %v err=%v", reentered, err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("skipped reentry changed status: %s", st.TaskStatus())
	}
}
