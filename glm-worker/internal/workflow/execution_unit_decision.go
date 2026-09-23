package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ExecuteDecisionWithExecutionUnitPayload(payload string) error {
	if !executionunit.IsPayload(payload) {
		return w.ExecuteDecisionWithExecutionMilestones(payload)
	}

	input, active, err := executionunit.Preflight(w.config, w.state, payload)
	if err != nil {
		return err
	}

	switch input.ExecutionUnit {
	case executionunit.ExecutionUnitSingle:
		return w.ExecuteDecision(input.Decision)
	case executionunit.ExecutionUnitMilestones:
		return w.executeMilestoneExecutionUnitDecision(input, active)
	default:
		return fmt.Errorf("unsupported execution-unit disposition %q", input.ExecutionUnit)
	}
}

func (w *Workflow) executeMilestoneExecutionUnitDecision(input executionunit.Decision, active bool) error {
	definitions := workflowMilestoneDefinitions(input.Milestones)
	if len(definitions) > 0 {
		activating := !active
		revision, err := ReviseExecutionMilestones(w.config, w.state, definitions, w.now().UTC())
		if err != nil {
			return err
		}
		if activating && definitions[revision.CurrentIndex].FreshWorker {
			if err := w.state.InvalidateSession(state.WorkerRole); err != nil {
				return err
			}
		}
		active = true
	}
	if !active {
		return fmt.Errorf("execution-unit milestones requires 2-8 milestone definitions or an existing pending milestone plan")
	}
	return w.ExecuteDecisionWithExecutionMilestones(input.Decision)
}

func workflowMilestoneDefinitions(definitions []executionunit.MilestoneDefinition) []ExecutionMilestoneDefinition {
	result := make([]ExecutionMilestoneDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = ExecutionMilestoneDefinition{
			ID:          definition.ID,
			Scope:       definition.Scope,
			Acceptance:  definition.Acceptance,
			FreshWorker: definition.FreshWorker,
		}
	}
	return result
}
