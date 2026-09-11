package app

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type ProjectContinuation = projectContinuationObligation

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool                `json:"completion_admitted"`
	StopAdmitted       bool                `json:"stop_admitted"`
	Continuation       ProjectContinuation `json:"continuation"`
}

const (
	projectContinuationReasonPostCompletionActive = "post-local-completion-active"
	projectContinuationActionStart                = "start"
)

func BuildParentRequestCompletionProjection(cfg config.AppConfig, st *state.StateStore) (ParentRequestCompletionProjection, error) {
	planContent, err := readProjectStatePlan(cfg.RepoRoot)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if planContent == nil {
		return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonPlanAbsent)), nil
	}
	goal, err := taskcontract.ParsePlanGoal(*planContent)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	schedule := taskcontract.ParsePlanSchedule(*planContent)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if goal.Present && goal.Status == taskcontract.GoalStatusCompleted {
		if len(active) != 0 || len(next) != 0 || len(blocked) != 0 {
			return ParentRequestCompletionProjection{}, fmt.Errorf("completed GOALではACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)", len(active), len(next), len(blocked))
		}
		if err := projectStateScheduleClosure(cfg.RepoRoot, schedule); err != nil {
			return ParentRequestCompletionProjection{}, err
		}
		return parentRequestProjection(projectContinuationObligation{
			State:  projectContinuationTerminal,
			Reason: projectContinuationReasonGoalCompleted,
		}), nil
	}
	if !goal.Present {
		return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonContinuationScopeUnbound)), nil
	}
	if len(active) > 1 {
		return ParentRequestCompletionProjection{}, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄が一意ではありません(%d件)", len(active))
	}
	if err := projectStateScheduleClosure(cfg.RepoRoot, schedule); err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	graph, err := buildProjectStateGraph(cfg.RepoRoot, append(append(append([]string{}, active...), next...), blocked...))
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if len(active) == 1 {
		return parentRequestProjection(projectContinuationObligation{
			State:          projectContinuationContinueNow,
			Task:           active[0],
			RequiredAction: projectContinuationActionStart,
			Reason:         projectContinuationReasonPostCompletionActive,
		}), nil
	}
	if runnable := projectStateNextRunnable(graph, next); runnable != nil {
		return parentRequestProjection(projectContinuationObligation{
			State:          projectContinuationContinueNow,
			Task:           *runnable,
			RequiredAction: projectContinuationActionStart,
			Reason:         projectContinuationReasonNextRunnable,
		}), nil
	}
	if blocker := firstProjectContinuationBlocker(projectStateBlockers(graph, next, blocked)); blocker != nil {
		return parentRequestProjection(projectContinuationObligation{
			State:   projectContinuationBlocked,
			Task:    blocker.Task,
			Reason:  blocker.Reason,
			Blocker: blocker,
		}), nil
	}
	return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonActiveTaskUnresolved)), nil
}

func parentRequestProjection(continuation ProjectContinuation) ParentRequestCompletionProjection {
	projection := ParentRequestCompletionProjection{Continuation: continuation}
	switch continuation.State {
	case projectContinuationTerminal:
		projection.CompletionAdmitted = true
		projection.StopAdmitted = true
	case projectContinuationBlocked, projectContinuationExplicitStop:
		projection.StopAdmitted = true
	}
	return projection
}
