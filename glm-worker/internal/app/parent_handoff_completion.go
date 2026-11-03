package app

import (
	"encoding/json"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func applyParentCompletionOperationHandoff(cfg config.AppConfig, output *parentHandoffOutput) {
	if !output.Consistent || output.RequiredAction == nil {
		return
	}
	if output.RequiredActionParameters["controller_command_json"] != "" {
		return
	}
	store, err := controller.Open(cfg)
	if err != nil {
		failParentCompletionHandoff(output, err)
		return
	}
	operation, err := store.ProjectParentCompletion()
	if err != nil {
		if *output.RequiredAction == controllerPublicationSurface {
			failParentCompletionHandoff(output, err)
		}
		return
	}
	applyParentOnlyOperationHandoff(output, operation)
	switch operation.Surface {
	case controller.PublicationOperationSurface:
		applyPublicationOperationHandoff(output, operation)
	case controller.FinalValidationSurface:
		applyFinalValidationOperationHandoff(output, operation)
	case controller.ExecutionOperationSurface:
		applyCompletionExecutionHandoff(output, operation)

	}
}

func applyParentOnlyOperationHandoff(output *parentHandoffOutput, operation controller.ParentCompletionOperation) {
	if operation.Mode != controller.ParentCompletionModeParentOnly || output.RequiredAction == nil {
		return
	}
	if operation.Surface != controllerPublicationSurface || *output.RequiredAction == controllerPublicationSurface {
		return
	}
	action := controllerPublicationSurface
	output.RequiredAction = &action
	output.AllowedActions = replaceParentAction(output.AllowedActions, canonicalStartAction, controllerPublicationSurface)
}

func replaceParentAction(actions []string, unwanted, replacement string) []string {
	projected := make([]string, 0, len(actions)+1)
	for _, action := range actions {
		if action == unwanted {
			continue
		}
		if !containsParentAction(projected, action) {
			projected = append(projected, action)
		}
	}
	if !containsParentAction(projected, replacement) {
		projected = append(projected, replacement)
	}
	return projected
}

func failParentCompletionHandoff(output *parentHandoffOutput, err error) {
	message := err.Error()
	output.Consistent = false
	output.Inconsistency = &message
	output.RequiredAction = nil
	output.RequiredActionParameters = nil
	output.AllowedActions = []string{"controller-evidence"}
}

func applyPublicationOperationHandoff(output *parentHandoffOutput, operation controller.ParentCompletionOperation) {
	if *output.RequiredAction != controllerPublicationSurface || operation.Command == nil {
		return
	}
	if output.RequiredActionParameters["controller_command_json"] != "" {
		return
	}
	command, err := json.Marshal(operation.Command)
	if err != nil {
		return
	}
	parameters := map[string]string{"controller_command_json": string(command)}
	if len(operation.SemanticSlots) != 0 {
		parameters["semantic_slot"] = strings.Join(operation.SemanticSlots, ",")
	}
	if len(operation.AllowedOperations) != 0 {
		parameters["completion_operations"] = strings.Join(operation.AllowedOperations, ",")
	}
	output.RequiredActionParameters = parameters
}

func applyFinalValidationOperationHandoff(output *parentHandoffOutput, operation controller.ParentCompletionOperation) {
	if *output.RequiredAction != controllerPublicationSurface || operation.FinalValidation == nil {
		return
	}
	action := controller.FinalValidationSurface
	output.RequiredAction = &action
	if !containsParentAction(output.AllowedActions, action) {
		output.AllowedActions = append(output.AllowedActions, action)
	}
	if !containsParentAction(output.AllowedActions, controllerPublicationSurface) {
		output.AllowedActions = append(output.AllowedActions, controllerPublicationSurface)
	}
	output.RequiredActionParameters = map[string]string{
		"form":        operation.FinalValidation.Form,
		"working_dir": operation.FinalValidation.WorkingDir,
	}
}

func applyCompletionExecutionHandoff(output *parentHandoffOutput, operation controller.ParentCompletionOperation) {
	if operation.Execution == nil {
		return
	}
	if *output.RequiredAction != controllerExecutionSurface && *output.RequiredAction != controllerPublicationSurface {
		return
	}
	command, err := json.Marshal(operation.Execution)
	if err != nil {
		failParentCompletionHandoff(output, err)
		return
	}
	applyControllerRecoveryHandoff(output, controllerExecutionSurface, string(command))
}
