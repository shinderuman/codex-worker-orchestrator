package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ExecutionMilestoneDefinition struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Acceptance  string `json:"acceptance"`
	FreshWorker bool   `json:"fresh_worker,omitempty"`
}

type ExecutionMilestoneRevision struct {
	Status         string `json:"status"`
	TaskID         string `json:"task_id"`
	CurrentIndex   int    `json:"current_index"`
	MilestoneCount int    `json:"milestone_count"`
	CurrentID      string `json:"current_id,omitempty"`
}

type executionTaskPlanInput struct {
	Request    string                         `json:"request"`
	Milestones []ExecutionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneInput struct {
	Milestones []ExecutionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneCompletion struct {
	CompletedAt        time.Time         `json:"completed_at"`
	CallID             string            `json:"call_id,omitempty"`
	WorkerSessionID    string            `json:"worker_session_id,omitempty"`
	Summary            string            `json:"summary"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	Snapshot           state.GitSnapshot `json:"snapshot"`
}

type executionMilestoneRecord struct {
	ExecutionMilestoneDefinition
	Status     string                        `json:"status"`
	Completion *executionMilestoneCompletion `json:"completion,omitempty"`
}

type executionMilestonePlan struct {
	Version            int                        `json:"version"`
	TaskID             string                     `json:"task_id"`
	ActiveTaskPath     string                     `json:"active_task_path"`
	TaskContractSHA256 string                     `json:"task_contract_sha256"`
	CurrentIndex       int                        `json:"current_index"`
	Milestones         []executionMilestoneRecord `json:"milestones"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}

type executionMilestonePrompt struct {
	TaskAuthority      string                              `json:"task_authority"`
	TaskContractSHA256 string                              `json:"task_contract_sha256"`
	Current            ExecutionMilestoneDefinition        `json:"current"`
	Completed          []executionMilestoneCompletedPrompt `json:"completed,omitempty"`
}

type executionMilestoneCompletedPrompt struct {
	ID                 string            `json:"id"`
	Summary            string            `json:"summary"`
	CallID             string            `json:"call_id,omitempty"`
	TaskContractSHA256 string            `json:"task_contract_sha256"`
	Snapshot           state.GitSnapshot `json:"snapshot"`
}

type executionMilestoneDecisionContext struct {
	request        string
	activeTaskPath string
	pocStage       bool
}

const (
	executionMilestonePlanVersion  = 1
	executionMilestonePending      = "pending"
	executionMilestoneComplete     = "complete"
	executionMilestoneMaxCount     = 8
	executionMilestoneMaxIDBytes   = 64
	executionMilestoneMaxTextBytes = 2048

	executionMilestonePromptBegin = "BEGIN_EXECUTION_MILESTONE_JSON"
	executionMilestonePromptEnd   = "END_EXECUTION_MILESTONE_JSON"
)

func ParseExecutionTaskPlanPayload(payload string) (string, []ExecutionMilestoneDefinition, error) {
	var input executionTaskPlanInput
	if err := decodeExecutionMilestoneJSON(payload, &input); err != nil {
		return "", nil, err
	}
	input.Request = strings.TrimSpace(input.Request)
	if input.Request == "" {
		return "", nil, fmt.Errorf("execution milestone task request is required")
	}
	if err := validateExecutionMilestoneDefinitions(input.Milestones); err != nil {
		return "", nil, err
	}
	return input.Request, input.Milestones, nil
}

func ParseExecutionMilestonePayload(payload string) ([]ExecutionMilestoneDefinition, error) {
	var input executionMilestoneInput
	if err := decodeExecutionMilestoneJSON(payload, &input); err != nil {
		return nil, err
	}
	if err := validateExecutionMilestoneDefinitions(input.Milestones); err != nil {
		return nil, err
	}
	return input.Milestones, nil
}

func decodeExecutionMilestoneJSON(payload string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("execution milestone payload is invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("execution milestone payload has trailing JSON")
	}
	return nil
}

func validateExecutionMilestoneDefinitions(definitions []ExecutionMilestoneDefinition) error {
	if len(definitions) < 2 || len(definitions) > executionMilestoneMaxCount {
		return fmt.Errorf("execution milestones require 2-%d entries", executionMilestoneMaxCount)
	}
	seen := make(map[string]struct{}, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Scope = strings.TrimSpace(definition.Scope)
		definition.Acceptance = strings.TrimSpace(definition.Acceptance)
		if err := validateExecutionMilestoneDefinition(*definition, seen); err != nil {
			return err
		}
		seen[definition.ID] = struct{}{}
	}
	return nil
}

func validateExecutionMilestoneDefinition(definition ExecutionMilestoneDefinition, seen map[string]struct{}) error {
	if definition.ID == "" || len(definition.ID) > executionMilestoneMaxIDBytes {
		return fmt.Errorf("execution milestone id must be 1-%d bytes", executionMilestoneMaxIDBytes)
	}
	if _, exists := seen[definition.ID]; exists {
		return fmt.Errorf("duplicate execution milestone id %q", definition.ID)
	}
	if definition.Scope == "" || len(definition.Scope) > executionMilestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q scope must be 1-%d bytes", definition.ID, executionMilestoneMaxTextBytes)
	}
	if definition.Acceptance == "" || len(definition.Acceptance) > executionMilestoneMaxTextBytes {
		return fmt.Errorf("execution milestone %q acceptance must be 1-%d bytes", definition.ID, executionMilestoneMaxTextBytes)
	}
	return nil
}

func executionMilestonePromptBlock(plan *executionMilestonePlan) (string, error) {
	prompt := executionMilestonePrompt{
		TaskAuthority:      plan.ActiveTaskPath,
		TaskContractSHA256: plan.TaskContractSHA256,
		Current:            plan.Milestones[plan.CurrentIndex].ExecutionMilestoneDefinition,
	}
	for _, record := range plan.Milestones[:plan.CurrentIndex] {
		if record.Status != executionMilestoneComplete || record.Completion == nil {
			return "", fmt.Errorf("completed execution milestone %q has no completion evidence", record.ID)
		}
		prompt.Completed = append(prompt.Completed, executionMilestoneCompletedPrompt{
			ID:                 record.ID,
			Summary:            record.Completion.Summary,
			CallID:             record.Completion.CallID,
			TaskContractSHA256: record.Completion.TaskContractSHA256,
			Snapshot:           record.Completion.Snapshot,
		})
	}
	data, err := json.Marshal(prompt)
	if err != nil {
		return "", fmt.Errorf("encode execution milestone prompt: %w", err)
	}
	return "\n\n" + executionMilestonePromptBegin + "\n" + string(data) + "\n" +
		"ACTIVE task remains task-wide authority; implement only current.scope and satisfy current.acceptance. Do not redo completed milestones merely because a new worker begins. If an explicit semantic amendment changed the ACTIVE task after a completed milestone, its recorded task_contract_sha256 will differ from the current task_contract_sha256; amendment work may touch that completed scope only when current.scope explicitly requires the semantic delta. Milestone completion never completes or weakens task-wide Acceptance.\n" +
		executionMilestonePromptEnd + "\n", nil
}

func replaceExecutionMilestonePromptBlock(prompt, block string) string {
	marker := "\n\n" + executionMilestonePromptBegin
	for {
		start := strings.Index(prompt, marker)
		if start < 0 {
			break
		}
		endRelative := strings.Index(prompt[start:], executionMilestonePromptEnd)
		if endRelative < 0 {
			break
		}
		end := start + endRelative + len(executionMilestonePromptEnd)
		for end < len(prompt) && prompt[end] == '\n' {
			end++
		}
		prompt = prompt[:start] + prompt[end:]
	}
	return strings.TrimRight(prompt, "\n") + block
}
