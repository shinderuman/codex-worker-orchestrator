package app

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffProjectsDefectBindingAction(t *testing.T) {
	taskPath := "IMPLEMENTATION_TASKS/follow-up.md"
	spec, ok := parentActionSpec(string(state.ParentActionBindDefectTask), map[string]string{"task": taskPath})
	if !ok {
		t.Fatal("bind-defect-task action spec was not projected")
	}
	wantCommand := []string{"glm-parent-action", "bind-defect-task", "--task", taskPath}
	if spec.Kind != "direct" || !reflect.DeepEqual(spec.Command, wantCommand) || spec.Parameters["task"] != taskPath {
		t.Fatalf("spec = %#v want command %#v", spec, wantCommand)
	}
	if _, ok := parentActionSpec(string(state.ParentActionBindDefectTask), nil); ok {
		t.Fatal("bind-defect-task projected without machine-bound task parameter")
	}
}

func TestPendingDefectBindingSuppressesMilestoneSideAction(t *testing.T) {
	status := string(state.TaskStatusWaitingSolReview)
	required := string(state.ParentActionBindDefectTask)
	actions := []string{string(state.ParentActionBindDefectTask)}
	projected := withExecutionMilestoneReconsideration(&status, &required, false, actions)
	if !reflect.DeepEqual(projected, actions) {
		t.Fatalf("projected actions = %#v want %#v", projected, actions)
	}
}
