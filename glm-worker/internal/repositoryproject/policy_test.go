package repositoryproject

import (
	"strings"
	"testing"
)

func TestPreparePostCompletionClassifiesNonGoalSchedule(t *testing.T) {
	cases := []struct {
		name    string
		active  []string
		next    []string
		blocked []string
		kind    PostCompletionKind
	}{
		{name: "promoted active", active: []string{"IMPLEMENTATION_TASKS/a.md"}, kind: PostCompletionContinue},
		{name: "promoted active with next", active: []string{"IMPLEMENTATION_TASKS/a.md"}, next: []string{"IMPLEMENTATION_TASKS/b.md"}, kind: PostCompletionContinue},
		{name: "blocked only", blocked: []string{"IMPLEMENTATION_TASKS/b.md"}, kind: PostCompletionBlocked},
		{name: "exhausted", kind: PostCompletionExhausted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := PreparePostCompletion(nonGoalPostCompletionPlan(tc.active, tc.next, tc.blocked))
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Kind != tc.kind {
				t.Fatalf("kind = %q want %q", prepared.Kind, tc.kind)
			}
		})
	}

	if _, err := PreparePostCompletion(nonGoalPostCompletionPlan(nil, []string{"IMPLEMENTATION_TASKS/b.md"}, nil)); err == nil ||
		!strings.Contains(err.Error(), "ACTIVE昇格済み") {
		t.Fatalf("promotion gap err = %v", err)
	}
	if _, err := PreparePostCompletion(nonGoalPostCompletionPlan(
		[]string{"IMPLEMENTATION_TASKS/a.md", "IMPLEMENTATION_TASKS/b.md"}, nil, nil)); err == nil ||
		!strings.Contains(err.Error(), "一意ではありません") {
		t.Fatalf("ambiguous active err = %v", err)
	}
}

func TestNonGoalCompletionSyncApplied(t *testing.T) {
	completed := "IMPLEMENTATION_TASKS/active.md"
	cases := []struct {
		name          string
		goalPresent   bool
		active        []string
		next          []string
		blocked       []string
		completedTask string
		applied       bool
	}{
		{name: "goal present skips identity", goalPresent: true, active: []string{completed}, completedTask: completed, applied: true},
		{name: "promoted active", active: []string{"IMPLEMENTATION_TASKS/next.md"}, completedTask: completed, applied: true},
		{name: "exhausted", completedTask: completed, applied: true},
		{name: "completed task still active", active: []string{completed}, completedTask: completed, applied: false},
		{name: "completed task relisted as next", next: []string{completed}, completedTask: completed, applied: false},
		{name: "completed task relisted as blocked", blocked: []string{completed}, completedTask: completed, applied: false},
		{name: "completed task identity missing", active: []string{"IMPLEMENTATION_TASKS/next.md"}, completedTask: "", applied: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared := PostCompletionPlan{Active: tc.active, Next: tc.next, Blocked: tc.blocked}
			prepared.Goal.Present = tc.goalPresent
			if got := NonGoalCompletionSyncApplied(prepared, tc.completedTask); got != tc.applied {
				t.Fatalf("applied = %t want %t", got, tc.applied)
			}
		})
	}
}

func TestPostCompletionProjectionAdmitsNonGoalShapes(t *testing.T) {
	promoted := "IMPLEMENTATION_TASKS/a.md"
	continueProjection := PostCompletionProjection(PostCompletionPlan{
		Kind: PostCompletionContinue, Active: []string{promoted},
	}, nil)
	if !continueProjection.CompletionAdmitted || !continueProjection.StopAdmitted ||
		continueProjection.Continuation.State != ContinuationContinueNow ||
		continueProjection.Continuation.Task != promoted ||
		continueProjection.Continuation.RequiredAction != ActionStart ||
		continueProjection.Continuation.Reason != ReasonPostCompletionActive {
		t.Fatalf("continue projection = %#v", continueProjection)
	}

	exhaustedProjection := PostCompletionProjection(PostCompletionPlan{Kind: PostCompletionExhausted}, nil)
	if !exhaustedProjection.CompletionAdmitted || !exhaustedProjection.StopAdmitted ||
		exhaustedProjection.Continuation.State != ContinuationTerminal ||
		exhaustedProjection.Continuation.Reason != ReasonScheduleExhausted {
		t.Fatalf("exhausted projection = %#v", exhaustedProjection)
	}

	blockedTask := "IMPLEMENTATION_TASKS/b.md"
	graph, err := BuildTaskGraph([]string{blockedTask}, map[string][]byte{
		blockedTask: []byte("# b\n\n## Dependencies\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	blockedProjection := PostCompletionProjection(PostCompletionPlan{
		Kind: PostCompletionBlocked, Blocked: []string{blockedTask},
	}, graph)
	if !blockedProjection.CompletionAdmitted || !blockedProjection.StopAdmitted ||
		blockedProjection.Continuation.State != ContinuationBlocked ||
		blockedProjection.Continuation.Task != blockedTask ||
		blockedProjection.Continuation.Reason != "blocked-section" ||
		blockedProjection.Continuation.RequiredAction != "" {
		t.Fatalf("blocked projection = %#v", blockedProjection)
	}

	blockedWithoutGraph := PostCompletionProjection(PostCompletionPlan{
		Kind: PostCompletionBlocked, Blocked: []string{blockedTask},
	}, nil)
	if blockedWithoutGraph.CompletionAdmitted || blockedWithoutGraph.StopAdmitted ||
		blockedWithoutGraph.Continuation.State != ContinuationUnknown {
		t.Fatalf("blocked projection without graph = %#v", blockedWithoutGraph)
	}

	ambiguousContinue := PostCompletionProjection(PostCompletionPlan{
		Kind:   PostCompletionContinue,
		Active: []string{"IMPLEMENTATION_TASKS/a.md", "IMPLEMENTATION_TASKS/b.md"},
	}, nil)
	if ambiguousContinue.CompletionAdmitted || ambiguousContinue.StopAdmitted ||
		ambiguousContinue.Continuation.State != ContinuationUnknown ||
		ambiguousContinue.Continuation.Reason != ReasonActiveTaskUnresolved {
		t.Fatalf("ambiguous continue projection = %#v", ambiguousContinue)
	}

	emptyContinue := PostCompletionProjection(PostCompletionPlan{Kind: PostCompletionContinue}, nil)
	if emptyContinue.CompletionAdmitted || emptyContinue.StopAdmitted ||
		emptyContinue.Continuation.State != ContinuationUnknown ||
		emptyContinue.Continuation.Reason != ReasonActiveTaskUnresolved {
		t.Fatalf("empty continue projection = %#v", emptyContinue)
	}
}

func TestPrepareParentCompletionHeadAdmitsNonGoalPostCompletionShapes(t *testing.T) {
	blockedPlan := nonGoalPostCompletionPlan(nil, nil, []string{"IMPLEMENTATION_TASKS/blocked.md"})
	prepared, err := PrepareParentCompletionHead(blockedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ActiveTask != "" || len(prepared.Tasks) != 1 || prepared.Tasks[0] != "IMPLEMENTATION_TASKS/blocked.md" {
		t.Fatalf("blocked-only non-goal head = %#v", prepared)
	}

	exhausted, err := PrepareParentCompletionHead(nonGoalPostCompletionPlan(nil, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.ActiveTask != "" || len(exhausted.Tasks) != 0 {
		t.Fatalf("exhausted non-goal head = %#v", exhausted)
	}

	promoted, err := PrepareParentCompletionHead(nonGoalPostCompletionPlan(
		[]string{"IMPLEMENTATION_TASKS/a.md"}, []string{"IMPLEMENTATION_TASKS/b.md"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if promoted.ActiveTask != "IMPLEMENTATION_TASKS/a.md" ||
		len(promoted.Tasks) != 2 || promoted.Tasks[0] != "IMPLEMENTATION_TASKS/a.md" || promoted.Tasks[1] != "IMPLEMENTATION_TASKS/b.md" {
		t.Fatalf("promoted non-goal head = %#v", promoted)
	}

	if _, err := PrepareParentCompletionHead(nonGoalPostCompletionPlan(nil, []string{"IMPLEMENTATION_TASKS/b.md"}, nil)); err == nil ||
		!strings.Contains(err.Error(), "ACTIVE昇格済み") {
		t.Fatalf("promotion gap err = %v", err)
	}
}

func nonGoalPostCompletionPlan(active, next, blocked []string) string {
	var body strings.Builder
	body.WriteString("# Plan\n\n## ACTIVE\n\n")
	writePostCompletionEntries(&body, active)
	body.WriteString("\n## NEXT（優先順）\n\n")
	writePostCompletionEntries(&body, next)
	body.WriteString("\n## BLOCKED / USER_PERMISSION_WAIT\n\n")
	writePostCompletionEntries(&body, blocked)
	return body.String()
}

func writePostCompletionEntries(body *strings.Builder, entries []string) {
	for _, entry := range entries {
		body.WriteString("- `")
		body.WriteString(entry)
		body.WriteString("`\n")
	}
}

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
