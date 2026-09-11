package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type projectContinuationObligation struct {
	State          string               `json:"state"`
	Task           string               `json:"task,omitempty"`
	RequiredAction string               `json:"required_action,omitempty"`
	Reason         string               `json:"reason"`
	Blocker        *projectStateBlocker `json:"blocker,omitempty"`
}

const (
	projectContinuationContinueNow  = "continue-now"
	projectContinuationBlocked      = "blocked"
	projectContinuationTerminal     = "terminal"
	projectContinuationExplicitStop = "explicit-stop"
	projectContinuationUnknown      = "unknown"
)

const (
	projectContinuationReasonPlanAbsent                  = "plan-absent"
	projectContinuationReasonProjectStateIncomplete      = "project-state-incomplete"
	projectContinuationReasonLifecycleInconsistent       = "lifecycle-inconsistent"
	projectContinuationReasonUserInterruption            = "user-interruption"
	projectContinuationReasonGoalCompleted               = "goal-completed"
	projectContinuationReasonGoalLifecycleInconsistent   = "goal-lifecycle-inconsistent"
	projectContinuationReasonActiveTaskUnresolved        = "active-task-unresolved"
	projectContinuationReasonActiveTaskNotStarted        = "active-task-not-started"
	projectContinuationReasonActiveTaskMismatch          = "active-task-mismatch"
	projectContinuationReasonCurrentTask                 = "current-task"
	projectContinuationReasonContinuationScopeUnbound    = "continuation-scope-unbound"
	projectContinuationReasonNextRunnable                = "next-runnable"
	projectContinuationReasonGoalAcceptancePending       = "goal-acceptance-pending"
	projectContinuationReasonCompletionStateInconsistent = "completion-state-inconsistent"
)

func deriveProjectContinuation(output projectStateOutput, st *state.StateStore) projectContinuationObligation {
	status := st.TaskStatus()
	if status == state.TaskStatusInterrupted {
		return interruptedProjectContinuation(output, st)
	}
	if !output.PlanPresent {
		return unknownProjectContinuation(projectContinuationReasonPlanAbsent)
	}
	if output.Goal == nil || output.Schedule == nil {
		return unknownProjectContinuation(projectContinuationReasonProjectStateIncomplete)
	}
	if output.Goal.Present && output.Goal.Status == taskcontract.GoalStatusCompleted {
		return terminalProjectContinuation(st)
	}
	if len(output.Schedule.Active) != 1 {
		return unknownProjectContinuation(projectContinuationReasonActiveTaskUnresolved)
	}
	return activeProjectContinuation(output, st, output.Schedule.Active[0])
}

func interruptedProjectContinuation(output projectStateOutput, st *state.StateStore) projectContinuationObligation {
	plan, err := st.ParentActionPlan()
	if err != nil || plan.RequiredAction != state.ParentActionResume {
		return unknownProjectContinuation(projectContinuationReasonLifecycleInconsistent)
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopInterrupted {
		return unknownProjectContinuation(projectContinuationReasonLifecycleInconsistent)
	}
	task := st.ReadOr("active-task", "")
	if task == "" && output.Schedule != nil && len(output.Schedule.Active) == 1 {
		task = output.Schedule.Active[0]
	}
	return projectContinuationObligation{
		State:          projectContinuationExplicitStop,
		Task:           task,
		RequiredAction: string(plan.RequiredAction),
		Reason:         projectContinuationReasonUserInterruption,
	}
}

func terminalProjectContinuation(st *state.StateStore) projectContinuationObligation {
	status := st.TaskStatus()
	if status != state.TaskStatusNone && status != state.TaskStatusComplete {
		return unknownProjectContinuation(projectContinuationReasonGoalLifecycleInconsistent)
	}
	plan, err := st.ParentActionPlan()
	if err != nil || plan.RequiredAction != state.ParentActionNone {
		return unknownProjectContinuation(projectContinuationReasonGoalLifecycleInconsistent)
	}
	return projectContinuationObligation{State: projectContinuationTerminal, Reason: projectContinuationReasonGoalCompleted}
}

func activeProjectContinuation(output projectStateOutput, st *state.StateStore, activeTask string) projectContinuationObligation {
	status := st.TaskStatus()
	pinned := st.ReadOr("active-task", "")
	if status == state.TaskStatusNone {
		if pinned != "" {
			return unknownProjectContinuation(projectContinuationReasonActiveTaskMismatch)
		}
		return projectContinuationObligation{
			State:  projectContinuationContinueNow,
			Task:   activeTask,
			Reason: projectContinuationReasonActiveTaskNotStarted,
		}
	}
	if pinned != activeTask {
		return unknownProjectContinuation(projectContinuationReasonActiveTaskMismatch)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return unknownProjectContinuation(projectContinuationReasonLifecycleInconsistent)
	}
	if status == state.TaskStatusRateLimited || status == state.TaskStatusProviderUnavailable {
		return projectContinuationObligation{
			State:          projectContinuationBlocked,
			Task:           activeTask,
			RequiredAction: string(plan.RequiredAction),
			Reason:         string(status),
		}
	}
	if status != state.TaskStatusComplete || plan.RequiredAction != state.ParentActionNone {
		return currentTaskProjectContinuation(activeTask, plan.RequiredAction)
	}
	if !output.Goal.Present {
		return unknownProjectContinuation(projectContinuationReasonContinuationScopeUnbound)
	}
	if output.NextRunnable != nil {
		return projectContinuationObligation{
			State:  projectContinuationContinueNow,
			Task:   *output.NextRunnable,
			Reason: projectContinuationReasonNextRunnable,
		}
	}
	if blocker := firstProjectContinuationBlocker(output.Blockers); blocker != nil {
		return projectContinuationObligation{
			State:   projectContinuationBlocked,
			Task:    blocker.Task,
			Reason:  blocker.Reason,
			Blocker: blocker,
		}
	}
	if output.Completion == nil {
		return unknownProjectContinuation(projectContinuationReasonProjectStateIncomplete)
	}
	if output.Completion.Ready {
		return projectContinuationObligation{
			State:  projectContinuationContinueNow,
			Task:   activeTask,
			Reason: projectContinuationReasonGoalAcceptancePending,
		}
	}
	if len(output.Completion.Unmet) != 0 {
		return projectContinuationObligation{
			State:  projectContinuationContinueNow,
			Task:   activeTask,
			Reason: output.Completion.Unmet[0],
		}
	}
	return unknownProjectContinuation(projectContinuationReasonCompletionStateInconsistent)
}

func currentTaskProjectContinuation(task string, action state.ParentAction) projectContinuationObligation {
	obligation := projectContinuationObligation{
		State:  projectContinuationContinueNow,
		Task:   task,
		Reason: projectContinuationReasonCurrentTask,
	}
	if action != state.ParentActionNone {
		obligation.RequiredAction = string(action)
	}
	return obligation
}

func firstProjectContinuationBlocker(blockers []projectStateBlocker) *projectStateBlocker {
	if len(blockers) == 0 {
		return nil
	}
	blocker := blockers[0]
	blocker.Outstanding = append([]string(nil), blocker.Outstanding...)
	return &blocker
}

func unknownProjectContinuation(reason string) projectContinuationObligation {
	return projectContinuationObligation{State: projectContinuationUnknown, Reason: reason}
}
