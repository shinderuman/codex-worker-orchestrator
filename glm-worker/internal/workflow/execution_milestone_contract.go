package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type executionMilestonePrompt struct {
	TaskAuthority      string                              `json:"task_authority"`
	TaskContractSHA256 string                              `json:"task_contract_sha256"`
	Current            executionunit.MilestoneDefinition   `json:"current"`
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
	executionMilestonePromptBegin = "BEGIN_EXECUTION_MILESTONE_JSON"
	executionMilestonePromptEnd   = "END_EXECUTION_MILESTONE_JSON"
)

func executionMilestonePromptBlock(plan *executionmilestone.Plan) (string, error) {
	prompt := executionMilestonePrompt{
		TaskAuthority:      plan.ActiveTaskPath,
		TaskContractSHA256: plan.TaskContractSHA256,
		Current:            plan.Milestones[plan.CurrentIndex].MilestoneDefinition,
	}
	for _, record := range plan.Milestones[:plan.CurrentIndex] {
		if record.Status != executionmilestone.StatusComplete || record.Completion == nil {
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
