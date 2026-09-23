package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

func TestParentRequestProjectionFromFocusedPreservesAutomation(t *testing.T) {
	request := parentcontinuation.Request{
		CompletionAdmitted: false,
		StopAdmitted:       true,
		Continuation: repositoryproject.Continuation{
			State:  repositoryproject.ContinuationDeferredByVerifiedAutomation,
			Reason: parentcontinuation.ReasonVerifiedAutomation,
		},
		Automation: &parentcontinuation.Automation{
			AutomationID: "wake-automation",
			ParentThread: "parent-thread",
			WakeThread:   "wake-thread",
			ResumeAtUTC:  "2026-09-11T02:00:00Z",
			WakeAtUTC:    "2026-09-11T02:02:00Z",
		},
	}

	projection := parentRequestProjectionFromFocused(request)
	if projection.CompletionAdmitted || !projection.StopAdmitted {
		t.Fatalf("admission = completion:%t stop:%t", projection.CompletionAdmitted, projection.StopAdmitted)
	}
	if projection.Continuation.State != request.Continuation.State || projection.Continuation.Reason != request.Continuation.Reason {
		t.Fatalf("continuation = %#v want %#v", projection.Continuation.Continuation, request.Continuation)
	}
	proof := projection.Continuation.Automation
	if proof == nil {
		t.Fatal("automation proof was dropped")
	}
	if proof.AutomationID != request.Automation.AutomationID ||
		proof.ParentThread != request.Automation.ParentThread ||
		proof.WakeThread != request.Automation.WakeThread ||
		proof.ResumeAtUTC != request.Automation.ResumeAtUTC ||
		proof.WakeAtUTC != request.Automation.WakeAtUTC {
		t.Fatalf("automation proof = %#v want %#v", proof, request.Automation)
	}
}
