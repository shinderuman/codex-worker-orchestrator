package repositoryproject

import "testing"

func TestDeriveTaskAttributionDistinguishesCurrentHandoverAndMismatch(t *testing.T) {
	current := DeriveTaskAttribution("IMPLEMENTATION_TASKS/current.md", "IMPLEMENTATION_TASKS/current.md", Continuation{
		State:          ContinuationContinueNow,
		Task:           "IMPLEMENTATION_TASKS/current.md",
		RequiredAction: "resume",
		Reason:         ReasonCurrentTask,
	})
	if !current.Matches || current.Handover || current.Reason != ReasonCurrentTask || current.LegalNextAction != "resume" {
		t.Fatalf("current attribution = %#v", current)
	}

	handover := DeriveTaskAttribution("IMPLEMENTATION_TASKS/done.md", "IMPLEMENTATION_TASKS/next.md", Continuation{
		State:          ContinuationContinueNow,
		Task:           "IMPLEMENTATION_TASKS/next.md",
		RequiredAction: ActionStart,
		Reason:         ReasonPostCompletionActive,
	})
	if handover.Matches || !handover.Handover || handover.Reason != ReasonPostCompletionActive || handover.LegalNextAction != ActionStart {
		t.Fatalf("handover attribution = %#v", handover)
	}

	mismatch := DeriveTaskAttribution("IMPLEMENTATION_TASKS/stale.md", "IMPLEMENTATION_TASKS/current.md", UnknownContinuation(ReasonActiveTaskMismatch))
	if mismatch.Matches || mismatch.Handover || mismatch.Reason != ReasonActiveTaskMismatch || mismatch.LegalNextAction != "" {
		t.Fatalf("mismatch attribution = %#v", mismatch)
	}
}

func TestDeriveTaskAttributionMarksUnstartedCurrentActive(t *testing.T) {
	attribution := DeriveTaskAttribution("", "IMPLEMENTATION_TASKS/current.md", Continuation{
		State:  ContinuationContinueNow,
		Task:   "IMPLEMENTATION_TASKS/current.md",
		Reason: ReasonActiveTaskNotStarted,
	})
	if attribution.Matches || attribution.Handover || attribution.Reason != ReasonActiveTaskNotStarted || attribution.LegalNextAction != ActionStart {
		t.Fatalf("unstarted attribution = %#v", attribution)
	}
}

func TestBindTaskAuthorityRejectsFalseHandover(t *testing.T) {
	attribution := DeriveTaskAttribution("IMPLEMENTATION_TASKS/stale.md", "IMPLEMENTATION_TASKS/next.md", Continuation{
		State:          ContinuationContinueNow,
		Task:           "IMPLEMENTATION_TASKS/next.md",
		RequiredAction: ActionStart,
		Reason:         ReasonPostCompletionActive,
	})
	bound := BindTaskAuthority(attribution, "IMPLEMENTATION_TASKS/done.md")
	if bound.AuthorityTask != "IMPLEMENTATION_TASKS/done.md" || bound.Handover || bound.Reason != ReasonActiveTaskMismatch || bound.LegalNextAction != "" {
		t.Fatalf("bound attribution = %#v", bound)
	}

	legal := BindTaskAuthority(DeriveTaskAttribution("IMPLEMENTATION_TASKS/done.md", "IMPLEMENTATION_TASKS/next.md", Continuation{
		State:          ContinuationContinueNow,
		Task:           "IMPLEMENTATION_TASKS/next.md",
		RequiredAction: ActionStart,
		Reason:         ReasonPostCompletionActive,
	}), "IMPLEMENTATION_TASKS/done.md")
	if legal.AuthorityTask != legal.LifecycleTask || !legal.Handover || legal.Reason != ReasonPostCompletionActive || legal.LegalNextAction != ActionStart {
		t.Fatalf("legal bound attribution = %#v", legal)
	}
}
