package app

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestCanonicalCompletionExecutionOverridesStartWithTypedOperation(t *testing.T) {
	for _, actionName := range []string{canonicalStartAction, controllerExecutionSurface, controllerPublicationSurface} {
		t.Run(actionName, func(t *testing.T) {
			action := actionName
			output := parentHandoffOutput{Consistent: true, RequiredAction: &action, AllowedActions: []string{action}}
			operation := controller.ParentCompletionOperation{
				Surface: controller.ExecutionOperationSurface,
				Operation: controller.PublicationOperationParentOnlyMaterialize,
				Execution: &controller.CompletionExecutionCommand{
					Action: controller.PublicationOperationParentOnlyMaterialize,
					ExpectedGeneration: 42,
				},
			}
			applyCompletionExecutionHandoff(&output, operation)
			if output.RequiredAction == nil || *output.RequiredAction != controllerExecutionSurface {
				t.Fatalf("typed operation was not admitted: %#v", output)
			}
			var command controller.CompletionExecutionCommand
			if err := json.Unmarshal([]byte(output.RequiredActionParameters["controller_command_json"]), &command); err != nil {
				t.Fatal(err)
			}
			if command.Action != controller.PublicationOperationParentOnlyMaterialize || command.ExpectedGeneration != 42 {
				t.Fatalf("canonical command changed: %#v", command)
			}
			if len(output.AllowedActions) != 1 || output.AllowedActions[0] != controllerExecutionSurface {
				t.Fatalf("non-canonical continuation survived: %#v", output.AllowedActions)
			}
		})
	}
}

func TestCompletionExecutionDoesNotInventOperation(t *testing.T) {
	action := canonicalStartAction
	output := parentHandoffOutput{Consistent: true, RequiredAction: &action}
	applyCompletionExecutionHandoff(&output, controller.ParentCompletionOperation{Surface: controller.ExecutionOperationSurface})
	if output.RequiredAction == nil || *output.RequiredAction != canonicalStartAction || output.RequiredActionParameters != nil {
		t.Fatalf("missing typed operation created a command: %#v", output)
	}
}
