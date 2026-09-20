package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffProjectsMilestoneReconsiderationAtNaturalBoundaries(t *testing.T) {
	statuses := []state.TaskStatus{
		state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusInterrupted,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			value := string(status)
			output := parentHandoffOutput{
				Version:        parentHandoffVersion,
				Consistent:     true,
				TaskStatus:     &value,
				AllowedActions: []string{string(state.ParentActionResume)},
			}
			data, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				AllowedActions []string                           `json:"allowed_actions"`
				ActionSpecs    map[string]parentHandoffActionSpec `json:"action_specs"`
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			milestoneAction := string(parentaction.ActionReviseMilestones)
			if !containsString(decoded.AllowedActions, milestoneAction) {
				t.Fatalf("allowed_actions = %v", decoded.AllowedActions)
			}
			spec, ok := decoded.ActionSpecs[milestoneAction]
			if !ok {
				t.Fatalf("action_specs = %#v", decoded.ActionSpecs)
			}
			if spec.Kind != "staged" || !reflect.DeepEqual(spec.PrepareCommand, []string{"glm-parent-action", "prepare", milestoneAction}) {
				t.Fatalf("milestone action spec = %#v", spec)
			}
		})
	}
}

func TestParentHandoffDoesNotAddDedicatedMilestoneReconsiderationOutsideNaturalBoundaries(t *testing.T) {
	statuses := []state.TaskStatus{
		state.TaskStatusNone,
		state.TaskStatusActive,
		state.TaskStatusWaitingDecision,
		state.TaskStatusAwaitingParentCompletion,
		state.TaskStatusComplete,
		state.TaskStatusQualityGateRecoverable,
		state.TaskStatusParked,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			value := string(status)
			actions := withExecutionMilestoneReconsideration(&value, nil, false, []string{string(state.ParentActionDecision)})
			if containsString(actions, string(parentaction.ActionReviseMilestones)) {
				t.Fatalf("unexpected milestone reconsideration for %s: %v", status, actions)
			}
		})
	}
}

func TestParentHandoffDefersMilestoneReconsiderationBehindBlockingParentWork(t *testing.T) {
	rateLimited := string(state.TaskStatusRateLimited)
	if actions := withExecutionMilestoneReconsideration(&rateLimited, nil, true, []string{string(state.ParentActionResume)}); containsString(actions, string(parentaction.ActionReviseMilestones)) {
		t.Fatalf("pending decision exposed milestone reconsideration: %v", actions)
	}

	waitingReview := string(state.TaskStatusWaitingSolReview)
	approveSurface := string(state.ParentActionApproveSurface)
	if actions := withExecutionMilestoneReconsideration(&waitingReview, &approveSurface, false, []string{approveSurface}); containsString(actions, string(parentaction.ActionReviseMilestones)) {
		t.Fatalf("required approve-surface exposed milestone reconsideration: %v", actions)
	}
}

func TestParentHandoffRecoveryProjectsSameMilestoneReconsiderationSurface(t *testing.T) {
	status := string(state.TaskStatusRateLimited)
	output := parentHandoffRecoveryOutput{
		Version:        parentHandoffVersion,
		Consistent:     true,
		TaskStatus:     &status,
		AllowedActions: []string{string(state.ParentActionResume)},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		AllowedActions []string                           `json:"allowed_actions"`
		ActionSpecs    map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	milestoneAction := string(parentaction.ActionReviseMilestones)
	if !containsString(decoded.AllowedActions, milestoneAction) {
		t.Fatalf("recovery allowed_actions = %v", decoded.AllowedActions)
	}
	if _, ok := decoded.ActionSpecs[milestoneAction]; !ok {
		t.Fatalf("recovery action_specs = %#v", decoded.ActionSpecs)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
