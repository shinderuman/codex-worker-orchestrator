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
		if semantic != runtimeDispositionReasonSlot {
			return parentaction.StagedTemplate{}, false, fmt.Errorf("typed execution semantic slot %s is unsupported", semantic)
		}
		payload.WriteString(runtimeDispositionReasonPrefix + parentaction.RuntimeDispositionReasonPlaceholder + "\n")
		slots = append(slots, parentaction.PreparedSlot{Name: semantic, Placeholder: parentaction.RuntimeDispositionReasonPlaceholder})
	}
	payload.WriteString(publicationCommandTemplatePrefix + command + "\n")
	return parentaction.StagedTemplate{Payload: []byte(payload.String()), Slots: slots}, true, nil
}

func mergeExecutionStagedPayload(payload []byte) ([]byte, error) {
	fields, err := parseCompletionStaging(payload, map[string]string{"RUNTIME_DISPOSITION_REASON": runtimeDispositionReasonSlot})
	if err != nil {
		return nil, err
	}
	command, err := fields.commandObject()
	if err != nil {
		return nil, err
	}
	if reason, ok := fields.Semantic[runtimeDispositionReasonSlot]; ok {
		encoded, err := json.Marshal(reason)
		if err != nil {
			return nil, err
		}
		var disposition map[string]json.RawMessage
		if err := json.Unmarshal(command["runtime_disposition"], &disposition); err != nil || disposition == nil {
			return nil, fmt.Errorf("runtime disposition reason lacks its machine-bound operation")
		}
		disposition["reason"] = encoded
		command["runtime_disposition"], err = json.Marshal(disposition)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(command)
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
		if strings.HasPrefix(line, runtimeDispositionReasonPrefix) {
			line = runtimeDispositionReasonPrefix + parentaction.RuntimeDispositionReasonPlaceholder
		}
		normalized.WriteString(line + "\n")
	}
	if normalized.String() != string(expected.Payload) {
		return fmt.Errorf("execution machine template was changed or became stale")
	}
	return nil
}
