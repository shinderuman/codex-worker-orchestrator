package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type executionUnitDecision struct {
	Decision      string
	ExecutionUnit string
	Milestones    []ExecutionMilestoneDefinition
}

const (
	executionUnitSingle     = "single"
	executionUnitMilestones = "milestones"

	executionUnitPrefix  = "EXECUTION_UNIT: "
	milestonesJSONPrefix = "MILESTONES_JSON: "
	decisionMarker       = "DECISION:"
)

func (w *Workflow) ExecuteDecisionWithExecutionUnitPayload(payload string) error {
	if !strings.HasPrefix(payload, executionUnitPrefix) {
		return w.ExecuteDecisionWithExecutionMilestones(payload)
	}

	input, err := parseExecutionUnitDecision(payload)
	if err != nil {
		return err
	}

	active, err := w.hasPendingExecutionMilestone()
	if err != nil {
		return err
	}

	switch input.ExecutionUnit {
	case executionUnitSingle:
		if active {
			return fmt.Errorf("execution-unit single cannot bypass pending execution milestones")
		}
		return w.ExecuteDecision(input.Decision)
	case executionUnitMilestones:
		if len(input.Milestones) > 0 {
			revision, err := ReviseExecutionMilestones(w.config, w.state, input.Milestones, w.now().UTC())
			if err != nil {
				return err
			}
			if input.Milestones[revision.CurrentIndex].FreshWorker {
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
	default:
		return fmt.Errorf("unsupported execution-unit disposition %q", input.ExecutionUnit)
	}
}

func parseExecutionUnitDecision(payload string) (executionUnitDecision, error) {
	parts := strings.SplitN(payload, "\n", 4)
	if len(parts) != 4 || !strings.HasPrefix(parts[0], executionUnitPrefix) ||
		!strings.HasPrefix(parts[1], milestonesJSONPrefix) || parts[2] != decisionMarker {
		return executionUnitDecision{}, fmt.Errorf("decision payload must use the machine-owned execution-unit template")
	}

	input := executionUnitDecision{
		ExecutionUnit: strings.TrimSpace(strings.TrimPrefix(parts[0], executionUnitPrefix)),
		Decision:      strings.TrimSpace(parts[3]),
	}
	if input.Decision == "" {
		return executionUnitDecision{}, fmt.Errorf("decision payload is empty")
	}

	milestones, err := parseExecutionUnitMilestones(parts[1])
	if err != nil {
		return executionUnitDecision{}, err
	}
	input.Milestones = milestones
	if err := validateExecutionUnitDecision(input); err != nil {
		return executionUnitDecision{}, err
	}
	return input, nil
}

func parseExecutionUnitMilestones(line string) ([]ExecutionMilestoneDefinition, error) {
	var input executionMilestoneInput
	payload := strings.TrimSpace(strings.TrimPrefix(line, milestonesJSONPrefix))
	if err := decodeExecutionMilestoneJSON(payload, &input); err != nil {
		return nil, err
	}
	return input.Milestones, nil
}

func validateExecutionUnitDecision(input executionUnitDecision) error {
	switch input.ExecutionUnit {
	case executionUnitSingle:
		if len(input.Milestones) != 0 {
			return fmt.Errorf("execution-unit single cannot include milestone definitions")
		}
	case executionUnitMilestones:
		if len(input.Milestones) > 0 {
			return validateExecutionMilestoneDefinitions(input.Milestones)
		}
	default:
		return fmt.Errorf("execution unit must be %q or %q", executionUnitSingle, executionUnitMilestones)
	}
	return nil
}
