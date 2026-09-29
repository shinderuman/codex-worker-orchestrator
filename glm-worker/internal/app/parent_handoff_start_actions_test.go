package app

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

func TestNewTaskHandoffProjectsSingleAndMilestoneStartActions(t *testing.T) {
	request := &ParentRequestCompletionProjection{
		TaskAttribution: repositoryproject.TaskAttribution{LegalNextAction: repositoryproject.ActionStart},
	}
	actions := withNewTaskExecutionUnitActions(request, nil)
	wantActions := []string{parentactiongrammar.StartAction, string(parentaction.ActionStartMilestones)}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("actions = %#v want %#v", actions, wantActions)
	}

	specs := parentActionSpecs(actions, nil)
	single := specs[parentactiongrammar.StartAction]
	if single.Kind != "direct" || !reflect.DeepEqual(single.Command, []string{"glm-parent-action", "start", "--execution-unit", "single"}) {
		t.Fatalf("single spec = %#v", single)
	}
	milestones := specs[string(parentaction.ActionStartMilestones)]
	if milestones.Kind != "staged" || !reflect.DeepEqual(milestones.PrepareCommand, []string{"glm-parent-action", "prepare", "start-milestones"}) {
		t.Fatalf("milestone spec = %#v", milestones)
	}
}

func TestNewTaskExecutionUnitActionsAreNotProjectedWithoutStartAdmission(t *testing.T) {
	request := &ParentRequestCompletionProjection{
		TaskAttribution: repositoryproject.TaskAttribution{LegalNextAction: "resume"},
	}
	original := []string{"resume"}
	got := withNewTaskExecutionUnitActions(request, original)
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("actions = %#v want %#v", got, original)
	}
}
