package workflow

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionMilestoneExplicitFixCompletesCurrentUnitBeforeFinalReview(t *testing.T) {
	w, st, runner, _ := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: implementedPacket("fixed first milestone")},
		{structured: implementedPacket("second milestone")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	})
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(w.config, st); err != nil {
		t.Fatal(err)
	}
	if err := w.captureQualitySurfaceBaseline(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, "IMPLEMENTATION_TASKS/large.md"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "quality surface requires semantic fix"); err != nil {
		t.Fatal(err)
	}
	definitions := []ExecutionMilestoneDefinition{
		{ID: "first", Scope: "finish first bounded unit", Acceptance: "first unit complete"},
		{ID: "second", Scope: "finish second bounded unit", Acceptance: "second unit complete"},
	}
	if err := w.initializeExecutionMilestones(definitions, "IMPLEMENTATION_TASKS/large.md"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteExplicitFixWithExecutionMilestones("repair the current milestone", "codex-review", ""); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-explicit-fix", "worker-milestone-2", "reviewer-1-high-floor", "reviewer-1-risk-floor"}
	if !reflect.DeepEqual(runner.phases, wantPhases) {
		t.Fatalf("phases = %v want %v", runner.phases, wantPhases)
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CurrentIndex != 2 || plan.Milestones[0].Completion == nil || plan.Milestones[0].Completion.Summary != "fixed first milestone" {
		t.Fatalf("plan after explicit fix = %#v", plan)
	}
}
