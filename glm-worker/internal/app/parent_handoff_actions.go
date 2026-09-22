package app

import (
	"encoding/json"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffActionSpec struct {
	Kind               string              `json:"kind"`
	Command            []string            `json:"command,omitempty"`
	PrepareCommand     []string            `json:"prepare_command,omitempty"`
	Parameters         map[string]string   `json:"parameters,omitempty"`
	RequiredParameters []string            `json:"required_parameters,omitempty"`
	OptionalParameters []string            `json:"optional_parameters,omitempty"`
	Choices            map[string][]string `json:"choices,omitempty"`
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
	if parentaction.Action(action) == parentaction.ActionReviseMilestones {
		return parentHandoffActionSpec{
			Kind:           "staged",
			PrepareCommand: []string{"glm-parent-action", "prepare", action},
		}, true
	}

	switch state.ParentAction(action) {
	case state.ParentActionDecision:
		return parentHandoffActionSpec{
			Kind:           "staged",
			PrepareCommand: []string{"glm-parent-action", "prepare", action},
		}, true
	case state.ParentActionFix:
		return parentFixActionSpec(requiredParameters), true
	case state.ParentActionImprovementDisposition:
		return improvementDispositionActionSpec(requiredParameters)
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
	case state.ParentActionBindDefectTask:
		taskPath := requiredParameters["task"]
		if taskPath == "" {
			return parentHandoffActionSpec{}, false
		}
		return parentHandoffActionSpec{
			Kind:       "direct",
			Command:    []string{"glm-parent-action", action, "--task", taskPath},
			Parameters: map[string]string{"task": taskPath},
		}, true
	case state.ParentActionReopen:
		return parentHandoffActionSpec{Kind: "direct", Command: []string{"glm-parent-action", "reopen"}}, true
	case state.ParentActionAccept,
		state.ParentActionComplete,
		state.ParentActionInstall,
		state.ParentActionResume,
		state.ParentActionPark,
		state.ParentActionUnpark,
		state.ParentActionNoGo:
		return parentHandoffActionSpec{Kind: "direct", Command: []string{"glm-parent-action", action}}, true
	default:
		return parentHandoffActionSpec{}, false
	}
}

func improvementDispositionActionSpec(requiredParameters map[string]string) (parentHandoffActionSpec, bool) {
	signalKind := requiredParameters[improvementSignalKindParameter]
	sourceCallID := requiredParameters[improvementSignalCallIDParameter]
	if signalKind == "" || sourceCallID == "" {
		return parentHandoffActionSpec{}, false
	}
	parameters := make(map[string]string, len(requiredParameters))
	for key, value := range requiredParameters {
		parameters[key] = value
	}
	return parentHandoffActionSpec{
		Kind: "bounded-choice",
		Command: []string{
			"glm-parent-action",
			string(state.ParentActionImprovementDisposition),
			"--signal-kind",
			signalKind,
			"--source-call-id",
			sourceCallID,
		},
		Parameters:         parameters,
		RequiredParameters: []string{"--disposition"},
		OptionalParameters: []string{"--task"},
		Choices: map[string][]string{
			"--disposition": state.ImprovementSignalDispositionChoices(),
		},
	}, true
}

func parentFixActionSpec(requiredParameters map[string]string) parentHandoffActionSpec {
	prepareCommand := []string{"glm-parent-action", "prepare", string(state.ParentActionFix)}
	optionalParameters := []string{"--origin", "--cause", "--accepted-scope"}
	var parameters map[string]string
	if acceptedScope := requiredParameters["accepted-scope"]; acceptedScope != "" {
		prepareCommand = append(prepareCommand, "--accepted-scope", acceptedScope)
		parameters = map[string]string{"accepted-scope": acceptedScope}
		optionalParameters = []string{"--origin", "--cause"}
	}
	return parentHandoffActionSpec{
		Kind:               "staged",
		PrepareCommand:     prepareCommand,
		Parameters:         parameters,
		OptionalParameters: optionalParameters,
	}
}
