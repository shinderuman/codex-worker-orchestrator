package parentactiongrammar

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/observationexec"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Spec struct {
	Kind               string              `json:"kind"`
	Command            []string            `json:"command,omitempty"`
	PrepareCommand     []string            `json:"prepare_command,omitempty"`
	Parameters         map[string]string   `json:"parameters,omitempty"`
	RequiredParameters []string            `json:"required_parameters,omitempty"`
	OptionalParameters []string            `json:"optional_parameters,omitempty"`
	Choices            map[string][]string `json:"choices,omitempty"`
}

const (
	Binary = "glm-parent-action"

	StartAction = "start"

	AcceptedScopeParameter = state.ParentActionAcceptedScopeParameter
	SignalKindParameter    = "signal-kind"
	SourceCallIDParameter  = "source-call-id"

	AcceptedScopeOption = "--accepted-scope"
	ExecutionUnitOption = "--execution-unit"
)

func Project(action string, requiredParameters map[string]string) (Spec, bool) {
	if parentaction.IsControllerAction(action) {
		return staged(action), true
	}
	if spec, handled := projectExecutionUnitAction(action); handled {
		return spec, true
	}

	switch state.ParentAction(action) {
	case state.ParentActionDecision:
		return staged(action), true
	case state.ParentActionObservationExecute:
		return projectObservationExecute(), true
	case state.ParentActionFix:
		return projectFix(requiredParameters)
	case state.ParentActionApproveSurface:
		return projectApproveSurface(requiredParameters)
	case state.ParentActionAccept,
		state.ParentActionResume:
		return direct(action), true
	default:
		return Spec{}, false
	}
}

func projectExecutionUnitAction(action string) (Spec, bool) {
	switch parentaction.Action(action) {
	case parentaction.Action(StartAction):
		return Spec{
			Kind:    "direct",
			Command: []string{Binary, StartAction, ExecutionUnitOption, "single"},
		}, true
	case parentaction.ActionStartMilestones, parentaction.ActionReviseMilestones:
		return staged(action), true
	default:
		return Spec{}, false
	}
}

func ValidateApproveSurfaceArgs(args []string) bool {
	return len(args) == 2 && args[0] == AcceptedScopeOption && args[1] == parentfix.AcceptedScopeCurrentDiff
}

func staged(action string) Spec {
	return Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", action}}
}

func projectObservationExecute() Spec {
	return Spec{
		Kind:               "staged",
		PrepareCommand:     []string{Binary, "prepare", string(state.ParentActionObservationExecute)},
		RequiredParameters: []string{"operation"},
		OptionalParameters: []string{"reference", "working-dir", "deadline-ms"},
		Choices: map[string][]string{
			"operation": observationexec.Operations(),
		},
	}
}

func direct(action string) Spec {
	return Spec{Kind: "direct", Command: []string{Binary, action}}
}

func projectFix(requiredParameters map[string]string) (Spec, bool) {
	spec := staged(string(state.ParentActionFix))
	acceptedScope := requiredParameters[AcceptedScopeParameter]
	if acceptedScope != "" {
		if acceptedScope != parentfix.AcceptedScopeCurrentDiff {
			return Spec{}, false
		}
		spec.PrepareCommand = append(spec.PrepareCommand, parentfix.AcceptedScopeOption, acceptedScope)
		spec.Parameters = map[string]string{AcceptedScopeParameter: acceptedScope}
	}
	spec.OptionalParameters = parentfix.OptionalArgumentNames(acceptedScope != "")
	return spec, true
}

func projectApproveSurface(requiredParameters map[string]string) (Spec, bool) {
	acceptedScope := requiredParameters[AcceptedScopeParameter]
	if acceptedScope != parentfix.AcceptedScopeCurrentDiff {
		return Spec{}, false
	}
	return Spec{
		Kind:       "direct",
		Command:    []string{Binary, string(state.ParentActionApproveSurface), AcceptedScopeOption, acceptedScope},
		Parameters: map[string]string{AcceptedScopeParameter: acceptedScope},
	}, true
}
