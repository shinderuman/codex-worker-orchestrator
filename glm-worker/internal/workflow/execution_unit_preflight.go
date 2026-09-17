package workflow

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func ValidateDecisionExecutionUnitPayload(cfg config.AppConfig, st *state.StateStore, payload string) error {
	w := &Workflow{config: cfg, state: st, now: time.Now}
	_, _, err := w.validateDecisionExecutionUnitPayload(payload)
	return err
}

func (w *Workflow) validateDecisionExecutionUnitPayload(payload string) (executionUnitDecision, bool, error) {
	input, err := parseExecutionUnitDecision(payload)
	if err != nil {
		return executionUnitDecision{}, false, err
	}
	active, err := w.hasPendingExecutionMilestone()
	if err != nil {
		return executionUnitDecision{}, false, err
	}

	switch input.ExecutionUnit {
	case executionUnitSingle:
		if active {
			return executionUnitDecision{}, false, fmt.Errorf("execution-unit single cannot bypass pending execution milestones")
		}
	case executionUnitMilestones:
		if len(input.Milestones) == 0 {
			if !active {
				return executionUnitDecision{}, false, fmt.Errorf("execution-unit milestones requires 2-8 milestone definitions or an existing pending milestone plan")
			}
			return input, active, nil
		}
		if err := validateExecutionMilestoneRevisionPreflight(w.config, w.state, input.Milestones, w.now().UTC()); err != nil {
			return executionUnitDecision{}, false, err
		}
		active = true
	default:
		return executionUnitDecision{}, false, fmt.Errorf("unsupported execution-unit disposition %q", input.ExecutionUnit)
	}
	return input, active, nil
}

func validateExecutionMilestoneRevisionPreflight(
	cfg config.AppConfig,
	st *state.StateStore,
	definitions []ExecutionMilestoneDefinition,
	now time.Time,
) error {
	if err := validateExecutionMilestoneDefinitions(definitions); err != nil {
		return err
	}
	if !executionMilestoneRevisionStatusAllowed(st.TaskStatus()) {
		return fmt.Errorf("execution milestones can only be revised at a stopped worker parent boundary")
	}
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		return err
	}
	if plan != nil {
		clone := *plan
		clone.Milestones = append([]executionMilestoneRecord(nil), plan.Milestones...)
		plan = &clone
	}
	_, err = prepareExecutionMilestoneRevision(cfg, st, plan, definitions, now.UTC())
	return err
}
