package workflow

import (
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ExecutionProgressProjection struct {
	Status              string                      `json:"status"`
	Band                string                      `json:"band,omitempty"`
	Basis               string                      `json:"basis"`
	Precision           string                      `json:"precision"`
	Reason              string                      `json:"reason,omitempty"`
	PhaseStage          string                      `json:"phase_stage,omitempty"`
	CompletedMilestones []string                    `json:"completed_milestones,omitempty"`
	CurrentMilestone    *ExecutionProgressMilestone `json:"current_milestone,omitempty"`
	PendingMilestones   []string                    `json:"pending_milestones,omitempty"`
}

type ExecutionProgressMilestone struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	Count    int    `json:"count"`
}

func ProjectExecutionProgress(st *state.StateStore, currentPhase, currentRole string) ExecutionProgressProjection {
	phaseStage := executionProgressPhaseStage(currentPhase, currentRole)
	plan, err := loadExecutionMilestonePlan(st)
	if err != nil {
		return ExecutionProgressProjection{
			Status:     "indeterminate",
			Basis:      "machine-state",
			Precision:  "unavailable",
			Reason:     "milestone-state-unavailable",
			PhaseStage: phaseStage,
		}
	}
	if plan == nil || len(plan.Milestones) == 0 {
		return ExecutionProgressProjection{
			Status:     "indeterminate",
			Basis:      "single-or-untracked-execution-unit",
			Precision:  "unavailable",
			Reason:     "no-execution-milestones",
			PhaseStage: phaseStage,
		}
	}

	projection := ExecutionProgressProjection{
		Status:     "estimated",
		Basis:      "execution-milestones+phase",
		Precision:  "coarse",
		PhaseStage: phaseStage,
	}
	for index, milestone := range plan.Milestones {
		switch {
		case index < plan.CurrentIndex:
			projection.CompletedMilestones = append(projection.CompletedMilestones, milestone.ID)
		case index == plan.CurrentIndex:
			projection.CurrentMilestone = &ExecutionProgressMilestone{
				ID:       milestone.ID,
				Position: index + 1,
				Count:    len(plan.Milestones),
			}
		case index > plan.CurrentIndex:
			projection.PendingMilestones = append(projection.PendingMilestones, milestone.ID)
		}
	}

	projection.Band = executionProgressBand(plan.CurrentIndex, len(plan.Milestones), phaseStage, st.TaskStatus())
	if st.TaskStatus() == state.TaskStatusComplete {
		projection.Status = "complete"
		projection.Precision = "exact"
	}
	return projection
}

func executionProgressPhaseStage(currentPhase, currentRole string) string {
	if currentRole == string(state.ReviewerRole) || strings.HasPrefix(currentPhase, "reviewer-") {
		return "review"
	}
	switch state.WorkerPhaseCategory(currentPhase) {
	case state.WorkerPhaseCategoryNew,
		state.WorkerPhaseCategoryExplicitFix,
		state.WorkerPhaseCategoryAutoFix:
		return "implementation"
	case state.WorkerPhaseCategoryDecision:
		return "decision"
	default:
		if currentPhase == "" {
			return "unknown"
		}
		return "other"
	}
}

func executionProgressBand(currentIndex, milestoneCount int, phaseStage string, taskStatus state.TaskStatus) string {
	if taskStatus == state.TaskStatusComplete {
		return "complete"
	}
	if currentIndex >= milestoneCount {
		return "late"
	}
	lastIndex := milestoneCount - 1
	if currentIndex == 0 {
		if phaseStage == "review" || phaseStage == "decision" {
			return "early-to-middle"
		}
		return "early"
	}
	if currentIndex == lastIndex {
		if phaseStage == "implementation" || phaseStage == "unknown" {
			return "middle-to-late"
		}
		return "late"
	}
	return "middle"
}
