package workflow

import (
	"fmt"
	"strings"
)

const (
	executionUnitSingle     = "single"
	executionUnitMilestones = "milestones"

	executionUnitPrefix = "EXECUTION_UNIT: "
	milestonesJSONPrefix = "MILESTONES_JSON: "
	decisionMarker       = "DECISION:"
)

type executionUnitDecision struct {
	Decision      string
	ExecutionUnit string
	Milestones    []ExecutionMilestoneDefinition
}

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
			if _, err := ReviseExecutionMilestones(w.config, w.state, input.Milestones, w.now().UTC()); err != nil {
				return err
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

	var milestoneInput executionMilestoneInput
	milestonesJSON := strings.TrimSpace(strings.TrimPrefix(parts[1], milestonesJSONPrefix))
	if err := decodeExecutionMilestoneJSON(milestonesJSON, &milestoneInput); err != nil {
		return executionUnitDecision{}, err
	}
	input.Milestones = milestoneInput.Milestones

	switch input.ExecutionUnit {
	case executionUnitSingle:
		if len(input.Milestones) != 0 {
			return executionUnitDecision{}, fmt.Errorf("execution-unit single cannot include milestone definitions")
		}
	case executionUnitMilestones:
		if len(input.Milestones) > 0 {
			if err := validateExecutionMilestoneDefinitions(input.Milestones); err != nil {
				return executionUnitDecision{}, err
			}
		}
	default:
		return executionUnitDecision{}, fmt.Errorf("execution unit must be %q or %q", executionUnitSingle, executionUnitMilestones)
	}
	return input, nil
}
