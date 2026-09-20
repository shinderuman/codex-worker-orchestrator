package app

import (
	"encoding/json"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffActionSpec struct {
	Kind           string            `json:"kind"`
	Command        []string          `json:"command,omitempty"`
	PrepareCommand []string          `json:"prepare_command,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
}

type parentHandoffOutputAlias parentHandoffOutput
type parentHandoffRecoveryOutputAlias parentHandoffRecoveryOutput

func (output parentHandoffOutput) MarshalJSON() ([]byte, error) {
	projected := output
	projected.AllowedActions = withExecutionMilestoneReconsideration(
		output.TaskStatus,
		output.RequiredAction,
		output.PendingDecision,
		output.AllowedActions,
	)
	return json.Marshal(struct {
		parentHandoffOutputAlias
		ActionSpecs map[string]parentHandoffActionSpec `json:"action_specs"`
	}{
		parentHandoffOutputAlias: parentHandoffOutputAlias(projected),
		ActionSpecs:              parentActionSpecs(projected.AllowedActions, projected.RequiredActionParameters),
	})
}

func (output parentHandoffRecoveryOutput) MarshalJSON() ([]byte, error) {
	projected := output
	projected.AllowedActions = withExecutionMilestoneReconsideration(
		output.TaskStatus,
		output.RequiredAction,
		output.PendingDecision,
		output.AllowedActions,
	)
	return json.Marshal(struct {
		parentHandoffRecoveryOutputAlias
		ActionSpecs map[string]parentHandoffActionSpec `json:"action_specs"`
	}{
		parentHandoffRecoveryOutputAlias: parentHandoffRecoveryOutputAlias(projected),
		ActionSpecs:                      parentActionSpecs(projected.AllowedActions, projected.RequiredActionParameters),
	})
}

func withExecutionMilestoneReconsideration(
	taskStatus *string,
	requiredAction *string,
	pendingDecision bool,
	actions []string,
) []string {
	projected := append([]string(nil), actions...)
	if pendingDecision || taskStatus == nil || !executionMilestoneReconsiderationStatus(state.TaskStatus(*taskStatus)) {
		return projected
	}
	if requiredAction != nil && *requiredAction == string(state.ParentActionApproveSurface) {
		return projected
	}
	milestoneAction := string(parentaction.ActionReviseMilestones)
	for _, action := range projected {
		if action == milestoneAction {
			return projected
		}
	}
	return append(projected, milestoneAction)
}

func executionMilestoneReconsiderationStatus(status state.TaskStatus) bool {
	switch status {
	case state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusInterrupted:
		return true
	default:
		return false
	}
}

func parentActionSpecs(actions []string, requiredParameters map[string]string) map[string]parentHandoffActionSpec {
	specs := make(map[string]parentHandoffActionSpec, len(actions))
	for _, action := range actions {
		if spec, ok := parentActionSpec(action, requiredParameters); ok {
			specs[action] = spec
		}
	}
	return specs
}

func parentActionSpec(action string, requiredParameters map[string]string) (parentHandoffActionSpec, bool) {
	if parentaction.Action(action) == parentaction.ActionReviseMilestones {
		return parentHandoffActionSpec{
			Kind:           "staged",
			PrepareCommand: []string{"glm-parent-action", "prepare", action},
		}, true
	}

	switch state.ParentAction(action) {
	case state.ParentActionDecision, state.ParentActionFix:
		return parentHandoffActionSpec{
			Kind:           "staged",
			PrepareCommand: []string{"glm-parent-action", "prepare", action},
		}, true
	case state.ParentActionApproveSurface:
		acceptedScope := requiredParameters["accepted-scope"]
		if acceptedScope == "" {
			return parentHandoffActionSpec{}, false
		}
		return parentHandoffActionSpec{
			Kind:       "direct",
			Command:    []string{"glm-parent-action", action, "--accepted-scope", acceptedScope},
			Parameters: map[string]string{"accepted-scope": acceptedScope},
		}, true
	case state.ParentActionReopen:
		return parentHandoffActionSpec{
			Kind:    "direct",
			Command: []string{"glm-parent-action", "reopen"},
		}, true
	case state.ParentActionAccept,
		state.ParentActionComplete,
		state.ParentActionInstall,
		state.ParentActionResume,
		state.ParentActionPark,
		state.ParentActionUnpark,
		state.ParentActionNoGo:
		return parentHandoffActionSpec{
			Kind:    "direct",
			Command: []string{"glm-parent-action", action},
		}, true
	default:
		return parentHandoffActionSpec{}, false
	}
}
