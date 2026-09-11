package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParentCompletionHeadAcceptsBlockedOnlyActiveGoal(t *testing.T) {
	root := newFinalHeadRepo(t)
	writeFinalHeadFile(t, root, implementationPlanFile, blockedCompletionInitialPlan())
	writeFinalHeadFile(t, root, "IMPLEMENTATION_TASKS/active.md", "# active\n\n## External feasibility\n\nstatus: not-applicable\n")
	writeFinalHeadFile(t, root, "IMPLEMENTATION_TASKS/blocked.md", "# blocked\n")
	commitFinalHeadFixture(t, root)

	if err := os.Remove(filepath.Join(root, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writeFinalHeadFile(t, root, implementationPlanFile, blockedCompletionFinalPlan())
	commitFinalHeadFixture(t, root)

	status, err := CheckParentCompletionHead(root)
	if err != nil || status != "plan completion head: verified" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if _, err := CheckFinalHeadPlan(root); err == nil {
		t.Fatal("ordinary final-head validation unexpectedly accepted blocked-only plan")
	}
}

func blockedCompletionInitialPlan() string {
	return "# plan\n\n## GOAL\n\nstatus: active\n\nGoal fixture\n\n" +
		"## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/blocked.md`\n"
}

func blockedCompletionFinalPlan() string {
	return "# plan\n\n## GOAL\n\nstatus: active\n\nGoal fixture\n\n" +
		"## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n- `IMPLEMENTATION_TASKS/blocked.md`\n"
}
