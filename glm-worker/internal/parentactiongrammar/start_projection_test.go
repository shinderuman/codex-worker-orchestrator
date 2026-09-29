package parentactiongrammar

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestProjectNewTaskExecutionUnitActions(t *testing.T) {
	single, ok := Project(StartAction, nil)
	if !ok {
		t.Fatal("single start action was not projected")
	}
	wantSingle := Spec{Kind: "direct", Command: []string{Binary, StartAction, ExecutionUnitOption, "single"}}
	if !reflect.DeepEqual(single, wantSingle) {
		t.Fatalf("single start spec = %#v want %#v", single, wantSingle)
	}

	milestones, ok := Project(string(parentaction.ActionStartMilestones), nil)
	if !ok {
		t.Fatal("milestone start action was not projected")
	}
	wantMilestones := Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", string(parentaction.ActionStartMilestones)}}
	if !reflect.DeepEqual(milestones, wantMilestones) {
		t.Fatalf("milestone start spec = %#v want %#v", milestones, wantMilestones)
	}
}
