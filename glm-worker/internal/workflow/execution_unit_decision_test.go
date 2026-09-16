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
		{structured: implementedPacket("first bounded unit complete")},
		{structured: implementedPacket("second bounded unit complete")},
		{structured: passPacket()},
	})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	if got := st.ReadOr("worker.id", ""); got == "" {
		t.Fatal("expected existing worker session before milestone activation")
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

	wantPhases := []string{"worker-new", "worker-decision", "worker-milestone-2", "reviewer-1"}
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
}

func TestExecutionUnitDecisionSingleKeepsLowOverheadPath(t *testing.T) {
	w, st, runner, _ := newExecutionMilestoneWorkflow(t, []runnerStep{
		{structured: needsSolDecisionPacket()},
		{structured: implementedPacket("resolved")},
		{structured: passPacket()},
	})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}
	payload := "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\nContinue as one bounded execution unit.\n"
	if err := w.ExecuteDecisionWithExecutionUnitPayload(payload); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-decision", "reviewer-1"}
	if !reflect.DeepEqual(runner.phases, wantPhases) {
		t.Fatalf("phases = %v want %v", runner.phases, wantPhases)
	}
	if _, err := os.Stat(st.Path(state.ExecutionMilestonesStateFile)); !os.IsNotExist(err) {
		t.Fatalf("single-unit decision created milestone state: %v", err)
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
