package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

const controllerExecutionAction = "controller-execution"
const runtimeDispositionReasonPrefix = "RUNTIME_DISPOSITION_REASON: "
const runtimeDispositionReasonSlot = "runtime_disposition.reason"
const executionTerminationReasonPrefix = "EXECUTION_TERMINATION_REASON: "
const executionTerminationReasonSlot = "execution_termination.reason"

func executionStagedTemplate(cfg config.AppConfig) (parentaction.StagedTemplate, bool, error) {
	var projection, diagnostics bytes.Buffer
	if err := runWorker(cfg.RepoRoot, []string{"--authority", "controller-execution-plan"}, nil, &projection, &diagnostics, nil); err != nil {
		return parentaction.StagedTemplate{}, false, fmt.Errorf("execution operation projection failed: %w: %s", err, diagnostics.String())
	}
	var handoff struct {
		Consistent     bool              `json:"consistent"`
		RequiredAction *string           `json:"required_action"`
		Parameters     map[string]string `json:"required_action_parameters"`
	}
	if err := json.Unmarshal(projection.Bytes(), &handoff); err != nil {
		return parentaction.StagedTemplate{}, false, err
	}
	if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionAction {
		return parentaction.StagedTemplate{}, false, nil
	}
	command := handoff.Parameters["controller_command_json"]
	if !handoff.Consistent || command == "" {
		return parentaction.StagedTemplate{}, false, fmt.Errorf("canonical execution continuation has no admitted typed command")
	}
	var payload strings.Builder
	slots := []parentaction.PreparedSlot{}
	if semantic := handoff.Parameters["semantic_slot"]; semantic != "" {
		prefix, placeholder, err := projectedExecutionReason(semantic, command)
		if err != nil {
			return parentaction.StagedTemplate{}, false, err
		}
		payload.WriteString(prefix + placeholder + "\n")
		slots = append(slots, parentaction.PreparedSlot{Name: semantic, Placeholder: placeholder})
	}
	payload.WriteString(publicationCommandTemplatePrefix + command + "\n")
	return parentaction.StagedTemplate{Payload: []byte(payload.String()), Slots: slots}, true, nil
}

func mergeExecutionStagedPayload(payload []byte) ([]byte, error) {
	fields, err := parseCompletionStaging(payload, map[string]string{"RUNTIME_DISPOSITION_REASON": runtimeDispositionReasonSlot, "EXECUTION_TERMINATION_REASON": executionTerminationReasonSlot})
	if err != nil {
		return nil, err
	}
	command, err := fields.commandObject()
	if err != nil {
		return nil, err
	}
	if len(fields.Semantic) > 1 {
		return nil, fmt.Errorf("execution command cannot declare multiple termination reasons")
	}
	for slot, reason := range fields.Semantic {
		if err := bindExecutionReason(command, slot, reason); err != nil {
			return nil, err
		}
	}
	return json.Marshal(command)
}

func projectedExecutionReason(semantic, command string) (string, string, error) {
	var operation struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(command), &operation); err != nil {
		return "", "", fmt.Errorf("typed execution command is invalid: %w", err)
	}
	switch {
	case semantic == runtimeDispositionReasonSlot && operation.Action == "dispose-runtime":
		return runtimeDispositionReasonPrefix, parentaction.RuntimeDispositionReasonPlaceholder, nil
	case semantic == executionTerminationReasonSlot && operation.Action == "user-terminate-execution":
		return executionTerminationReasonPrefix, parentaction.ExecutionTerminationReasonPlaceholder, nil
	default:
		return "", "", fmt.Errorf("typed execution semantic slot %s is not declared for action %s", semantic, operation.Action)
	}
}

func executionReasonTarget(slot, action string) (string, error) {
	switch {
	case slot == runtimeDispositionReasonSlot && action == "dispose-runtime":
		return "runtime_disposition", nil
	case slot == executionTerminationReasonSlot && action == "user-terminate-execution":
		return "execution_termination", nil
	default:
		return "", fmt.Errorf("execution reason %s is not bound to action %s", slot, action)
	}
}

func bindExecutionReason(command map[string]json.RawMessage, slot, reason string) error {
	var action string
	if err := json.Unmarshal(command["action"], &action); err != nil {
		return fmt.Errorf("typed execution action is invalid: %w", err)
	}
	field, err := executionReasonTarget(slot, action)
	if err != nil {
		return err
	}
	var target map[string]json.RawMessage
	if err := json.Unmarshal(command[field], &target); err != nil || target == nil {
		return fmt.Errorf("execution reason %s lacks its machine-bound operation", slot)
	}
	if raw := target["reason"]; len(raw) != 0 {
		var previous string
		if err := json.Unmarshal(raw, &previous); err != nil || previous != "" {
			return fmt.Errorf("execution reason %s would overwrite machine authority", slot)
		}
	}
	target["reason"], err = json.Marshal(reason)
	if err != nil {
		return err
	}
	command[field], err = json.Marshal(target)
	return err
}

func validateExecutionStagedBinding(cfg config.AppConfig, payload []byte) error {
	expected, selected, err := executionStagedTemplate(cfg)
	if err != nil {
		return err
	}
	if !selected {
		return fmt.Errorf("execution staging no longer matches the legal operation")
	}
	var normalized strings.Builder
	for _, line := range strings.Split(string(payload), "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, runtimeDispositionReasonPrefix):
			line = runtimeDispositionReasonPrefix + parentaction.RuntimeDispositionReasonPlaceholder
		case strings.HasPrefix(line, executionTerminationReasonPrefix):
			line = executionTerminationReasonPrefix + parentaction.ExecutionTerminationReasonPlaceholder
		}
		normalized.WriteString(line + "\n")
	}
	if normalized.String() != string(expected.Payload) {
		return fmt.Errorf("execution machine template was changed or became stale")
	}
	return nil
}
