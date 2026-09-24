package app

import (
	"encoding/json"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffActionSpec = parentactiongrammar.Spec

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
	improvementSignal := projectImprovementSignal(&projected)
	return json.Marshal(struct {
		parentHandoffOutputAlias
		ActionSpecs       map[string]parentHandoffActionSpec `json:"action_specs"`
		ImprovementSignal *parentHandoffImprovementSignal    `json:"improvement_signal,omitempty"`
	}{
		parentHandoffOutputAlias: parentHandoffOutputAlias(projected),
		ActionSpecs:              parentActionSpecs(projected.AllowedActions, projected.RequiredActionParameters),
		ImprovementSignal:        improvementSignal,
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
	improvementSignal := projectRecoveryImprovementSignal(&projected)
	return json.Marshal(struct {
		parentHandoffRecoveryOutputAlias
		ActionSpecs       map[string]parentHandoffActionSpec `json:"action_specs"`
		ImprovementSignal *parentHandoffImprovementSignal    `json:"improvement_signal,omitempty"`
	}{
		parentHandoffRecoveryOutputAlias: parentHandoffRecoveryOutputAlias(projected),
		ActionSpecs:                      parentActionSpecs(projected.AllowedActions, projected.RequiredActionParameters),
		ImprovementSignal:                improvementSignal,
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
	if requiredAction != nil && (*requiredAction == string(state.ParentActionApproveSurface) || *requiredAction == string(state.ParentActionBindDefectTask)) {
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
	return parentactiongrammar.Project(action, requiredParameters)
}

func improvementDispositionActionSpec(requiredParameters map[string]string) (parentHandoffActionSpec, bool) {
	return parentactiongrammar.Project(string(state.ParentActionImprovementDisposition), requiredParameters)
}
