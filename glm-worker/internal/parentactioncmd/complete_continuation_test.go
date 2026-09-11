package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompleteReturnsImmediateParentContinuation(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitContinuationCompletionPlan(t, fixture, continuationPromotedPlan(), false)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil || output.ParentRequest.CompletionAdmitted || output.ParentRequest.StopAdmitted ||
		output.ParentRequest.Continuation.State != "continue-now" ||
		output.ParentRequest.Continuation.Task != "IMPLEMENTATION_TASKS/next.md" ||
		output.ParentRequest.Continuation.RequiredAction != "start" {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
}

func TestCompleteReturnsBlockedParentStopWithoutCompletion(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitContinuationCompletionPlan(t, fixture, continuationBlockedPlan(), false)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil || output.ParentRequest.CompletionAdmitted || !output.ParentRequest.StopAdmitted ||
		output.ParentRequest.Continuation.State != "blocked" ||
		output.ParentRequest.Continuation.Task != "IMPLEMENTATION_TASKS/next.md" {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
}

func TestCompleteAdmitsParentCompletionOnlyForTerminalGoal(t *testing.T) {
	fixture := newCompleteFixture(t)
	commitContinuationCompletionPlan(t, fixture, continuationTerminalPlan(), true)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.ParentRequest == nil || !output.ParentRequest.CompletionAdmitted || !output.ParentRequest.StopAdmitted ||
		output.ParentRequest.Continuation.State != "terminal" {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}
}

func commitContinuationCompletionPlan(t *testing.T, fixture *completeFixture, plan string, removeNext bool) {
	t.Helper()
	if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	if removeNext {
		if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS", "next.md")); err != nil {
			t.Fatal(err)
		}
	}
	writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", plan)
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "completion continuation sync")
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
}

func continuationPromotedPlan() string {
	return "# plan\n\n## GOAL\n\nstatus: active\n\nGoal fixture\n\n" +
		"## ACTIVE\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n"
}

func continuationBlockedPlan() string {
	return "# plan\n\n## GOAL\n\nstatus: active\n\nGoal fixture\n\n" +
		"## ACTIVE\n\n## NEXT（優先順）\n\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/next.md`\n"
}

func continuationTerminalPlan() string {
	return "# plan\n\n## GOAL\n\nstatus: completed\n\nGoal fixture\n\n" +
		"## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n"
}
