package workflow

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecutionUnitDecisionActivatesMilestonesAtNaturalBoundary(t *testing.T) {
	w, st, runner, _ := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: needsSolDecisionPacket()},
		{structured: implementedPacketWithRisk("first bounded unit complete", "HIGH")},
		{structured: implementedPacketWithRisk("second bounded unit complete", "HIGH")},
		{structured: needsSolReviewPacket()},
	})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	if err := st.Write("worker.id", "existing-worker"); err != nil {
		t.Fatal(err)
	}
	runner.onRun = func() {
		if len(runner.prompts) == 2 {
			if got := st.ReadOr("worker.id", ""); got != "" {
				t.Fatalf("fresh milestone activation reused worker session %q", got)
			}
		}
	}

	payload := "EXECUTION_UNIT: milestones\n" +
		`MILESTONES_JSON: {"milestones":[{"id":"first","scope":"finish first responsibility","acceptance":"first complete","fresh_worker":true},{"id":"second","scope":"finish second responsibility","acceptance":"second complete"}]}` + "\n" +
		"DECISION:\nSplit the remaining implementation into two coherent units.\n"
	if err := w.ExecuteDecisionWithExecutionUnitPayload(payload); err != nil {
		t.Fatal(err)
	}

	wantPhases := []string{"worker-new", "worker-decision", "worker-milestone-2", "reviewer-1-high-floor"}
	if !reflect.DeepEqual(runner.phases, wantPhases) {
		t.Fatalf("phases = %v want %v", runner.phases, wantPhases)
	}
	if len(runner.prompts) < 3 || !strings.Contains(runner.prompts[1], `"id":"first"`) || !strings.Contains(runner.prompts[2], `"id":"second"`) {
		t.Fatalf("milestone prompts were not activated at the decision boundary: %#v", runner.prompts)
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.CurrentIndex != 2 || len(plan.Milestones) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := os.Stat(st.Path(state.ExecutionMilestonesStateFile)); err != nil {
		t.Fatalf("durable milestone state missing: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("final status = %s", st.TaskStatus())
	}
}

func TestExecutionUnitDecisionSingleKeepsLowOverheadPath(t *testing.T) {
	w, st, runner, _ := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: needsSolDecisionPacket()},
		{structured: implementedPacketWithRisk("resolved", "HIGH")},
		{structured: needsSolReviewPacket()},
	})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	payload := "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\nContinue as one bounded execution unit.\n"
	if err := w.ExecuteDecisionWithExecutionUnitPayload(payload); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-decision", "reviewer-1-high-floor"}
	if !reflect.DeepEqual(runner.phases, wantPhases) {
		t.Fatalf("phases = %v want %v", runner.phases, wantPhases)
	}
	if _, err := os.Stat(st.Path(state.ExecutionMilestonesStateFile)); !os.IsNotExist(err) {
		t.Fatalf("single-unit decision created milestone state: %v", err)
	}
}

func TestExecutionUnitDecisionSingleCanReconsiderIntoMilestones(t *testing.T) {
	w, st, runner, _ := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: needsSolDecisionPacket()},
		{structured: implementedPacketWithRisk("single unit expanded materially", "HIGH")},
		{structured: needsSolReviewPacket()},
		{structured: implementedPacket("first remaining milestone complete")},
		{structured: implementedPacket("second remaining milestone complete")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	payload := "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\nContinue as one bounded execution unit.\n"
	if err := w.ExecuteDecisionWithExecutionUnitPayload(payload); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("single disposition did not reach natural review boundary: %s", st.TaskStatus())
	}
	if len(runner.prompts) != 3 {
		t.Fatalf("single path model calls = %d", len(runner.prompts))
	}

	definitions := []ExecutionMilestoneDefinition{
		{ID: "remaining-a", Scope: "finish first remaining responsibility", Acceptance: "first remaining responsibility complete"},
		{ID: "remaining-b", Scope: "finish second remaining responsibility", Acceptance: "second remaining responsibility complete"},
	}
	revision, err := ReviseExecutionMilestones(w.config, st, definitions, w.now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if revision.CurrentIndex != 0 || revision.CurrentID != "remaining-a" || revision.MilestoneCount != 2 {
		t.Fatalf("revision = %+v", revision)
	}
	if len(runner.prompts) != 3 {
		t.Fatalf("milestone reconsideration added a model call: %d", len(runner.prompts))
	}
	if got, err := st.TaskID(); err != nil || got != taskID {
		t.Fatalf("semantic task identity changed: got=%q err=%v want=%q", got, err, taskID)
	}

	if err := w.ExecuteExplicitFixWithExecutionMilestones("continue the remaining work through the revised milestones", "codex-review", "worker", ""); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{
		"worker-new",
		"worker-decision",
		"reviewer-1-high-floor",
		"worker-explicit-fix",
		"worker-milestone-2",
		"reviewer-1-high-floor",
		"reviewer-1-risk-floor",
	}
	if !reflect.DeepEqual(runner.phases, wantPhases) {
		t.Fatalf("phases = %v want %v", runner.phases, wantPhases)
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil || plan.CurrentIndex != 2 || len(plan.Milestones) != 2 {
		t.Fatalf("reconsidered plan = %#v", plan)
	}
	if plan.Milestones[0].Completion == nil || plan.Milestones[0].Completion.Summary != "first remaining milestone complete" ||
		plan.Milestones[1].Completion == nil || plan.Milestones[1].Completion.Summary != "second remaining milestone complete" {
		t.Fatalf("reconsidered milestone completions = %#v", plan.Milestones)
	}
	if got, err := st.TaskID(); err != nil || got != taskID {
		t.Fatalf("task identity changed after milestone continuation: got=%q err=%v want=%q", got, err, taskID)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task-wide final review boundary was not preserved: %s", st.TaskStatus())
	}
}

func TestParseExecutionUnitDecisionFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{name: "legacy free text", payload: "continue"},
		{name: "missing disposition", payload: "EXECUTION_UNIT: \nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\ncontinue"},
		{name: "single with milestones", payload: "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[{\"id\":\"a\",\"scope\":\"a\",\"acceptance\":\"a\"},{\"id\":\"b\",\"scope\":\"b\",\"acceptance\":\"b\"}]}\nDECISION:\ncontinue"},
		{name: "milestones with one definition", payload: "EXECUTION_UNIT: milestones\nMILESTONES_JSON: {\"milestones\":[{\"id\":\"a\",\"scope\":\"a\",\"acceptance\":\"a\"}]}\nDECISION:\nsplit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseExecutionUnitDecision(tc.payload); err == nil {
				t.Fatalf("invalid payload accepted: %q", tc.payload)
			}
		})
	}
}
