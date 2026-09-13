package repositoryproject

import (
	"strings"
	"testing"
)

func TestPrepareProjectStateCompletedGoalRequiresEmptySchedule(t *testing.T) {
	plan := "## GOAL\n\nstatus: completed\n\n## ACTIVE\n\n## NEXT\n\n## BLOCKED\n"
	prepared, err := PrepareProjectState(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Goal.Present || prepared.Goal.Status != "completed" || len(prepared.Active) != 0 || len(prepared.Next) != 0 || len(prepared.Blocked) != 0 {
		t.Fatalf("prepared completed goal = %#v", prepared)
	}

	plan = strings.Replace(plan, "## ACTIVE\n", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/a.md`\n", 1)
	if _, err := PrepareProjectState(plan); err == nil {
		t.Fatal("non-empty completed goal was accepted")
	}
}

func TestPrepareParentCompletionHeadAdmitsBlockedOnlyPlan(t *testing.T) {
	plan := "## GOAL\n\nstatus: active\n\n## ACTIVE\n\n## NEXT\n\n## BLOCKED\n\n- `IMPLEMENTATION_TASKS/blocked.md`\n"
	prepared, err := PrepareParentCompletionHead(plan)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ActiveTask != "" || len(prepared.Tasks) != 1 || prepared.Tasks[0] != "IMPLEMENTATION_TASKS/blocked.md" {
		t.Fatalf("blocked-only preparation = %#v", prepared)
	}
	if _, err := PrepareFinalHead(plan); err == nil {
		t.Fatal("ordinary final-head policy unexpectedly accepted blocked-only Plan")
	}
}

func TestTaskGraphDerivesRunnableAndBlocker(t *testing.T) {
	contents := map[string][]byte{
		"IMPLEMENTATION_TASKS/a.md": []byte("# a\n\n## Dependencies\n"),
		"IMPLEMENTATION_TASKS/b.md": []byte("# b\n\n## Dependencies\n\n- `IMPLEMENTATION_TASKS/a.md`\n"),
	}
	graph, err := BuildTaskGraph([]string{"IMPLEMENTATION_TASKS/a.md", "IMPLEMENTATION_TASKS/b.md"}, contents)
	if err != nil {
		t.Fatal(err)
	}
	if runnable := graph.NextRunnable([]string{"IMPLEMENTATION_TASKS/a.md", "IMPLEMENTATION_TASKS/b.md"}); runnable == nil || *runnable != "IMPLEMENTATION_TASKS/a.md" {
		t.Fatalf("next runnable = %v", runnable)
	}
	blockers := graph.Blockers([]string{"IMPLEMENTATION_TASKS/b.md"}, nil)
	if len(blockers) != 1 || len(blockers[0].Outstanding) != 1 || blockers[0].Outstanding[0] != "IMPLEMENTATION_TASKS/a.md" {
		t.Fatalf("blockers = %#v", blockers)
	}
}

func TestDeriveContinuationUsesProjectPolicyAfterLifecycleSnapshot(t *testing.T) {
	next := "IMPLEMENTATION_TASKS/next.md"
	project := ContinuationProjectView{
		PlanPresent:  true,
		ProjectReady: true,
		GoalPresent:  true,
		Active:       []string{"IMPLEMENTATION_TASKS/active.md"},
		NextRunnable: &next,
		Completion:   &CompletionView{Ready: false},
	}
	lifecycle := ContinuationLifecycle{
		PinnedTask:        "IMPLEMENTATION_TASKS/active.md",
		TaskComplete:      true,
		ParentActionKnown: true,
		NoRequiredAction:  true,
	}
	continuation := DeriveContinuation(project, lifecycle)
	if continuation.State != ContinuationContinueNow || continuation.Task != next || continuation.Reason != ReasonNextRunnable {
		t.Fatalf("continuation = %#v", continuation)
	}

	project.GoalCompleted = true
	lifecycle.GoalTerminalCompatible = true
	continuation = DeriveContinuation(project, lifecycle)
	if continuation.State != ContinuationTerminal || continuation.Reason != ReasonGoalCompleted {
		t.Fatalf("terminal continuation = %#v", continuation)
	}
}
