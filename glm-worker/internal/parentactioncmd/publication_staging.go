package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

const (
	controllerPublicationAction = "controller-publication"

	publicationCommandTemplatePrefix    = "COMMAND_TEMPLATE: "
	publicationMessagePrefix            = "MESSAGE: "
	publicationRequireInstallPrefix     = "REQUIRE_INSTALL: "
	publicationFailureDispositionPrefix = "FAILURE_DISPOSITION: "
)

var publicationSemanticLabels = map[string]string{
	"MESSAGE":             controller.CompletionSlotMessage,
	"REQUIRE_INSTALL":     controller.CompletionSlotRequireInstall,
	"FAILURE_DISPOSITION": controller.CompletionSlotFailureDisposition,
}

func publicationStagedTemplate(cfg config.AppConfig, selected string) (parentaction.StagedTemplate, error) {
	operation, err := projectControllerCompletionOperation(cfg, selected)
	if err != nil {
		return parentaction.StagedTemplate{}, err
	}
	if operation.Surface != controller.PublicationOperationSurface || operation.Command == nil {
		return parentaction.StagedTemplate{}, fmt.Errorf("controller publication has no machine-determined operation: %s", completionOperationReason(operation))
	}
	command, err := json.Marshal(operation.Command)
	if err != nil {
		return parentaction.StagedTemplate{}, err
	}
	var payload strings.Builder
	slots := []parentaction.PreparedSlot{}
	for _, semantic := range operation.SemanticSlots {
		switch semantic {
		case controller.CompletionSlotMessage:
			payload.WriteString(publicationMessagePrefix + parentaction.PublicationMessagePlaceholder + "\n")
			slots = append(slots, parentaction.PreparedSlot{Name: parentaction.PublicationMessageSlotName, Placeholder: parentaction.PublicationMessagePlaceholder})
		case controller.CompletionSlotRequireInstall:
			payload.WriteString(publicationRequireInstallPrefix + parentaction.PublicationRequireInstallPlaceholder + "\n")
			slots = append(slots, parentaction.PreparedSlot{Name: parentaction.PublicationRequireInstallSlotName, Placeholder: parentaction.PublicationRequireInstallPlaceholder, Choices: []string{"true", "false"}})
		case controller.CompletionSlotFailureDisposition:
			payload.WriteString(publicationFailureDispositionPrefix + parentaction.PublicationFailureDispositionPlaceholder + "\n")
			slots = append(slots, parentaction.PreparedSlot{Name: semantic, Placeholder: parentaction.PublicationFailureDispositionPlaceholder, Choices: []string{completionRetry, completionReenter}})
		default:
			return parentaction.StagedTemplate{}, fmt.Errorf("controller publication operation declares unknown semantic slot %q", semantic)
		}
	}
	payload.WriteString(publicationCommandTemplatePrefix + string(command) + "\n")
	return parentaction.StagedTemplate{Payload: []byte(payload.String()), Slots: slots}, nil
}

func completionOperationReason(operation controller.ParentCompletionOperation) string {
	if operation.Reason != "" {
		return operation.Reason
	}
	return operation.Operation
}

func projectControllerCompletion(cfg config.AppConfig) (controller.ParentCompletionOperation, error) {
	return projectControllerCompletionOperation(cfg, "")
}

func projectControllerCompletionOperation(cfg config.AppConfig, selected string) (controller.ParentCompletionOperation, error) {
	exists, err := controller.Exists(cfg)
	if err != nil {
		return controller.ParentCompletionOperation{}, err
	}
	if !exists {
		return controller.ParentCompletionOperation{}, fmt.Errorf("controller publication staging requires existing controller authority")
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controller.ParentCompletionOperation{}, err
	}
	return store.ProjectParentCompletionOperation(selected)
}

func mergePublicationStagedPayload(payload []byte) ([]byte, error) {
	fields, err := parseCompletionStaging(payload, publicationSemanticLabels)
	if err != nil {
		return nil, err
	}
	command, err := fields.commandObject()
	if err != nil {
		return nil, err
	}
	for name, value := range fields.Semantic {
		if err := applyPublicationSemanticValue(command, name, value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(command)
}

func applyPublicationSemanticValue(command map[string]json.RawMessage, name, value string) error {
	switch name {
	case controller.CompletionSlotMessage:
		if strings.Contains(value, parentaction.PublicationMessagePlaceholder) {
			return fmt.Errorf("publication staged message slot was not supplied")
		}
		encoded, err := json.Marshal(value)
		command["message"] = encoded
		return err
	case controller.CompletionSlotRequireInstall:
		return applyPublicationInstallObligation(command, value)
	case controller.CompletionSlotFailureDisposition:
		if value != completionRetry && value != completionReenter {
			return fmt.Errorf("completion failure disposition must be retry or reenter")
		}
		encoded, err := json.Marshal(value)
		command["failure_disposition"] = encoded
		return err
	default:
		return fmt.Errorf("publication semantic slot is unsupported: %s", name)
	}
}

func applyPublicationInstallObligation(command map[string]json.RawMessage, value string) error {
	if value != "true" && value != "false" {
		return fmt.Errorf("publication install obligation must be true or false")
	}
	var policy map[string]json.RawMessage
	if err := json.Unmarshal(command["policy"], &policy); err != nil || policy == nil {
		return fmt.Errorf("publication install obligation lacks a machine-bound policy")
	}
	policy["require_install"] = json.RawMessage(value)
	encoded, err := json.Marshal(policy)
	command["policy"] = encoded
	return err
}

func validatePreparedCompletionBinding(cfg config.AppConfig, payload, _ []byte) error {
	fields, err := parseCompletionStaging(payload, publicationSemanticLabels)
	if err != nil {
		return err
	}
	var staged controller.PublicationParentCommand
	decoder := json.NewDecoder(bytes.NewReader(fields.Template))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&staged); err != nil {
		return err
	}
	operation, err := projectControllerCompletionOperation(cfg, staged.Action)
	if err != nil {
		return err
	}
	if operation.Command == nil || !reflect.DeepEqual(staged, *operation.Command) {
		return fmt.Errorf("completion machine template was changed or became stale")
	}
	return fields.validateSemanticSlots(operation.SemanticSlots)
}

func (fields completionStagingFields) validateSemanticSlots(expected []string) error {
	if len(fields.Semantic) != len(expected) {
		return fmt.Errorf("completion semantic slots differ from the legal operation")
	}
	for _, name := range expected {
		if _, ok := fields.Semantic[name]; !ok {
			return fmt.Errorf("completion is missing declared semantic slot: %s", name)
		}
	}
	return nil
}
