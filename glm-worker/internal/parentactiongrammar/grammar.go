package parentactiongrammar

import (
	"fmt"

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

	RecordDefectFindingAction    = "record-defect-finding"
	ImprovementDispositionAction = "improvement-disposition"

	AcceptedScopeParameter = "accepted-scope"
	TaskParameter          = "task"
	SignalKindParameter    = "signal-kind"
	SourceCallIDParameter  = "source-call-id"

	AcceptedScopeOption = "--accepted-scope"
	TaskOption          = "--task"
	SignalKindOption    = "--signal-kind"
	SourceCallIDOption  = "--source-call-id"
	DispositionOption   = "--disposition"
)

func Project(action string, requiredParameters map[string]string) (Spec, bool) {
	if parentaction.Action(action) == parentaction.ActionReviseMilestones {
		return staged(action), true
	}

	switch state.ParentAction(action) {
	case state.ParentActionDecision:
		return staged(action), true
	case state.ParentActionFix:
		return projectFix(requiredParameters)
	case state.ParentActionImprovementDisposition:
		return projectImprovementDisposition(requiredParameters)
	case state.ParentActionApproveSurface:
		return projectApproveSurface(requiredParameters)
	case state.ParentActionBindDefectTask:
		return projectBindDefectTask(requiredParameters)
	case state.ParentActionReopen:
		return direct("reopen"), true
	case state.ParentActionAccept,
		state.ParentActionComplete,
		state.ParentActionInstall,
		state.ParentActionResume,
		state.ParentActionPark,
		state.ParentActionUnpark,
		state.ParentActionNoGo:
		return direct(action), true
	default:
		return Spec{}, false
	}
}

func ValidateApproveSurfaceArgs(args []string) bool {
	return len(args) == 2 && args[0] == AcceptedScopeOption && args[1] == parentfix.AcceptedScopeCurrentDiff
}

func ParseDefectRegistrationArgs(args []string) (string, string, bool) {
	if len(args) != 3 || args[1] != TaskOption {
		return "", "", false
	}
	if args[0] != RecordDefectFindingAction && args[0] != string(state.ParentActionBindDefectTask) {
		return "", "", false
	}
	return args[0], args[2], true
}

func ParseImprovementDispositionArgs(args []string) (string, string, string, string, error) {
	if !validImprovementDispositionShape(args) {
		return "", "", "", "", improvementDispositionUsageError()
	}
	kind := args[2]
	sourceCallID := args[4]
	if kind == "" || sourceCallID == "" {
		return "", "", "", "", improvementDispositionUsageError()
	}
	disposition := args[6]
	targetTask, err := improvementDispositionTarget(args)
	if err != nil {
		return "", "", "", "", err
	}
	if err := validateImprovementDispositionTarget(disposition, targetTask); err != nil {
		return "", "", "", "", err
	}
	return kind, sourceCallID, disposition, targetTask, nil
}

func validImprovementDispositionShape(args []string) bool {
	if len(args) != 7 && len(args) != 9 {
		return false
	}
	return args[0] == ImprovementDispositionAction &&
		args[1] == SignalKindOption &&
		args[3] == SourceCallIDOption &&
		args[5] == DispositionOption
}

func improvementDispositionTarget(args []string) (string, error) {
	if len(args) == 7 {
		return "", nil
	}
	if args[7] != TaskOption {
		return "", improvementDispositionUsageError()
	}
	return args[8], nil
}

func validateImprovementDispositionTarget(disposition, targetTask string) error {
	resolved := state.ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	needsTask := state.ImprovementDispositionNeedsTask(resolved)
	if needsTask && targetTask == "" {
		return fmt.Errorf("improvement signal disposition %s requires --task", disposition)
	}
	if !needsTask && targetTask != "" {
		return fmt.Errorf("improvement signal disposition %s does not accept --task", disposition)
	}
	return nil
}

func staged(action string) Spec {
	return Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", action}}
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

func projectBindDefectTask(requiredParameters map[string]string) (Spec, bool) {
	taskPath := requiredParameters[TaskParameter]
	if taskPath == "" {
		return Spec{}, false
	}
	return Spec{
		Kind:       "direct",
		Command:    []string{Binary, string(state.ParentActionBindDefectTask), TaskOption, taskPath},
		Parameters: map[string]string{TaskParameter: taskPath},
	}, true
}

func projectImprovementDisposition(requiredParameters map[string]string) (Spec, bool) {
	signalKind := requiredParameters[SignalKindParameter]
	sourceCallID := requiredParameters[SourceCallIDParameter]
	if signalKind == "" || sourceCallID == "" {
		return Spec{}, false
	}
	parameters := make(map[string]string, len(requiredParameters))
	for key, value := range requiredParameters {
		parameters[key] = value
	}
	return Spec{
		Kind: "bounded-choice",
		Command: []string{
			Binary,
			ImprovementDispositionAction,
			SignalKindOption,
			signalKind,
			SourceCallIDOption,
			sourceCallID,
		},
		Parameters:         parameters,
		RequiredParameters: []string{DispositionOption},
		OptionalParameters: []string{TaskOption},
		Choices: map[string][]string{
			DispositionOption: state.ImprovementSignalDispositionChoices(),
		},
	}, true
}

func improvementDispositionUsageError() error {
	return fmt.Errorf("usage: glm-parent-action improvement-disposition --signal-kind <kind> --source-call-id <call-id> --disposition <adopt|existing-owner|duplicate|reject|awaiting-evidence> [--task <IMPLEMENTATION_TASKS/...md>]")
}
