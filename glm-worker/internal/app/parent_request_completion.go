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

type parentRequestSchedule struct {
	goal     taskcontract.PlanGoal
	schedule taskcontract.PlanSchedule
	active   []string
	next     []string
	blocked  []string
}

const (
	projectContinuationReasonPostCompletionActive = "post-local-completion-active"
	projectContinuationActionStart                = "start"
)

func BuildCurrentParentRequestCompletionProjection(cfg config.AppConfig, st *state.StateStore) (ParentRequestCompletionProjection, error) {
	status := st.TaskStatus()
	if status == state.TaskStatusAwaitingParentCompletion || status == state.TaskStatusComplete {
		return BuildParentRequestCompletionProjection(cfg)
	}
	output, err := buildProjectState(cfg, st)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return parentRequestProjection(output.Continuation), nil
}

func BuildParentRequestCompletionProjection(cfg config.AppConfig) (ParentRequestCompletionProjection, error) {
	planContent, err := readProjectStatePlan(cfg.RepoRoot)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if planContent == nil {
		return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonPlanAbsent)), nil
	}
	parsed, err := parseParentRequestSchedule(*planContent)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	if parsed.goal.Present && parsed.goal.Status == taskcontract.GoalStatusCompleted {
		return completedParentRequestProjection(cfg.RepoRoot, parsed)
	}
	if !parsed.goal.Present {
		return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonContinuationScopeUnbound)), nil
	}
	return activeParentRequestProjection(cfg.RepoRoot, parsed)
}

func parseParentRequestSchedule(plan string) (parentRequestSchedule, error) {
	goal, err := taskcontract.ParsePlanGoal(plan)
	if err != nil {
		return parentRequestSchedule{}, err
	}
	schedule := taskcontract.ParsePlanSchedule(plan)
	active, err := schedule.ActiveEntries()
	if err != nil {
		return parentRequestSchedule{}, err
	}
	next, blocked, err := schedule.NonActiveEntries()
	if err != nil {
		return parentRequestSchedule{}, err
	}
	return parentRequestSchedule{goal: goal, schedule: schedule, active: active, next: next, blocked: blocked}, nil
}

func completedParentRequestProjection(repoRoot string, parsed parentRequestSchedule) (ParentRequestCompletionProjection, error) {
	if len(parsed.active) != 0 || len(parsed.next) != 0 || len(parsed.blocked) != 0 {
		return ParentRequestCompletionProjection{}, fmt.Errorf(
			"completed GOALではACTIVE/NEXT/BLOCKEDを空にする必要があります(active=%d next=%d blocked=%d)",
			len(parsed.active), len(parsed.next), len(parsed.blocked),
		)
	}
	if err := projectStateScheduleClosure(repoRoot, parsed.schedule); err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return parentRequestProjection(projectContinuationObligation{
		State:  projectContinuationTerminal,
		Reason: projectContinuationReasonGoalCompleted,
	}), nil
}

func activeParentRequestProjection(repoRoot string, parsed parentRequestSchedule) (ParentRequestCompletionProjection, error) {
	if len(parsed.active) > 1 {
		return ParentRequestCompletionProjection{}, fmt.Errorf("IMPLEMENTATION_PLAN.local.mdのACTIVE欄が一意ではありません(%d件)", len(parsed.active))
	}
	if err := projectStateScheduleClosure(repoRoot, parsed.schedule); err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	entries := append(append(append([]string{}, parsed.active...), parsed.next...), parsed.blocked...)
	graph, err := buildProjectStateGraph(repoRoot, entries)
	if err != nil {
		return ParentRequestCompletionProjection{}, err
	}
	return derivePostCompletionProjection(graph, parsed), nil
}

func derivePostCompletionProjection(graph *projectStateGraph, parsed parentRequestSchedule) ParentRequestCompletionProjection {
	if len(parsed.active) == 1 {
		return parentRequestProjection(projectContinuationObligation{
			State:          projectContinuationContinueNow,
			Task:           parsed.active[0],
			RequiredAction: projectContinuationActionStart,
			Reason:         projectContinuationReasonPostCompletionActive,
		})
	}
	if runnable := projectStateNextRunnable(graph, parsed.next); runnable != nil {
		return parentRequestProjection(projectContinuationObligation{
			State:          projectContinuationContinueNow,
			Task:           *runnable,
			RequiredAction: projectContinuationActionStart,
			Reason:         projectContinuationReasonNextRunnable,
		})
	}
	if blocker := firstProjectContinuationBlocker(projectStateBlockers(graph, parsed.next, parsed.blocked)); blocker != nil {
		return parentRequestProjection(projectContinuationObligation{
			State:   projectContinuationBlocked,
			Task:    blocker.Task,
			Reason:  blocker.Reason,
			Blocker: blocker,
		})
	}
	return parentRequestProjection(unknownProjectContinuation(projectContinuationReasonActiveTaskUnresolved))
}

func parentRequestProjection(continuation ProjectContinuation) ParentRequestCompletionProjection {
	projection := ParentRequestCompletionProjection{Continuation: continuation}
	switch continuation.State {
	case projectContinuationTerminal:
		projection.CompletionAdmitted = true
		projection.StopAdmitted = true
	case projectContinuationBlocked:
		projection.StopAdmitted = continuation.Reason != string(state.TaskStatusRateLimited)
	case projectContinuationDeferredByVerifiedAutomation, projectContinuationExplicitStop:
		projection.StopAdmitted = true
	}
	return projection
}
