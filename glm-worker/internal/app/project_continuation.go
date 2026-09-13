package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type projectContinuationObligation struct {
	State          string                         `json:"state"`
	Task           string                         `json:"task,omitempty"`
	RequiredAction string                         `json:"required_action,omitempty"`
	Reason         string                         `json:"reason"`
	Blocker        *projectStateBlocker           `json:"blocker,omitempty"`
	Automation     *projectContinuationAutomation `json:"automation,omitempty"`
}

const (
	projectContinuationContinueNow                  = repositoryproject.ContinuationContinueNow
	projectContinuationBlocked                      = repositoryproject.ContinuationBlocked
	projectContinuationTerminal                     = repositoryproject.ContinuationTerminal
	projectContinuationExplicitStop                 = repositoryproject.ContinuationExplicitStop
	projectContinuationDeferredByVerifiedAutomation = repositoryproject.ContinuationDeferredByVerifiedAutomation
	projectContinuationUnknown                      = repositoryproject.ContinuationUnknown
)

const (
	projectContinuationReasonPlanAbsent                  = repositoryproject.ReasonPlanAbsent
	projectContinuationReasonProjectStateIncomplete      = repositoryproject.ReasonProjectStateIncomplete
	projectContinuationReasonLifecycleInconsistent       = repositoryproject.ReasonLifecycleInconsistent
	projectContinuationReasonUserInterruption            = repositoryproject.ReasonUserInterruption
	projectContinuationReasonGoalCompleted               = repositoryproject.ReasonGoalCompleted
	projectContinuationReasonGoalLifecycleInconsistent   = repositoryproject.ReasonGoalLifecycleInconsistent
	projectContinuationReasonActiveTaskUnresolved        = repositoryproject.ReasonActiveTaskUnresolved
	projectContinuationReasonActiveTaskNotStarted        = repositoryproject.ReasonActiveTaskNotStarted
	projectContinuationReasonActiveTaskMismatch          = repositoryproject.ReasonActiveTaskMismatch
	projectContinuationReasonCurrentTask                 = repositoryproject.ReasonCurrentTask
	projectContinuationReasonContinuationScopeUnbound    = repositoryproject.ReasonContinuationScopeUnbound
	projectContinuationReasonNextRunnable                = repositoryproject.ReasonNextRunnable
	projectContinuationReasonGoalAcceptancePending       = repositoryproject.ReasonGoalAcceptancePending
	projectContinuationReasonCompletionStateInconsistent = repositoryproject.ReasonCompletionStateInconsistent
)

func deriveProjectContinuation(output projectStateOutput, st *state.StateStore) projectContinuationObligation {
	project := repositoryproject.ContinuationProjectView{PlanPresent: output.PlanPresent}
	if output.Goal != nil && output.Schedule != nil {
		project.ProjectReady = true
		project.GoalPresent = output.Goal.Present
		project.GoalCompleted = output.Goal.Present && output.Goal.Status == taskcontract.GoalStatusCompleted
		project.Active = append([]string(nil), output.Schedule.Active...)
		project.NextRunnable = cloneStringPointer(output.NextRunnable)
		project.Blockers = cloneProjectBlockers(output.Blockers)
		if output.Completion != nil {
			project.Completion = &repositoryproject.CompletionView{
				Ready: output.Completion.Ready,
				Unmet: append([]string(nil), output.Completion.Unmet...),
			}
		}
	}
	return projectContinuationFromPolicy(repositoryproject.DeriveContinuation(project, continuationLifecycle(st)))
}

func continuationLifecycle(st *state.StateStore) repositoryproject.ContinuationLifecycle {
	status := st.TaskStatus()
	pinned := st.ReadOr("active-task", "")
	lifecycle := repositoryproject.ContinuationLifecycle{
		Interrupted:  status == state.TaskStatusInterrupted,
		PinnedTask:   pinned,
		TaskAbsent:   status == state.TaskStatusNone,
		TaskComplete: status == state.TaskStatusComplete,
	}
	plan, planErr := st.ParentActionPlan()
	planKnown := planErr == nil
	if planKnown {
		lifecycle.ParentActionKnown = true
		lifecycle.RequiredAction = string(plan.RequiredAction)
		lifecycle.NoRequiredAction = plan.RequiredAction == state.ParentActionNone
	}
	lifecycle.InterruptedResumeValid = interruptedResumeValid(st, status, planKnown, plan.RequiredAction)
	lifecycle.TemporaryBlockReason = continuationTemporaryBlockReason(status)
	lifecycle.GoalTerminalCompatible = goalTerminalCompatible(status, pinned, planKnown, plan.RequiredAction)
	return lifecycle
}

func interruptedResumeValid(st *state.StateStore, status state.TaskStatus, planKnown bool, action state.ParentAction) bool {
	if status != state.TaskStatusInterrupted || !planKnown || action != state.ParentActionResume {
		return false
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	return err == nil && checkpoint.StopKind == state.ResumeStopInterrupted
}

func continuationTemporaryBlockReason(status state.TaskStatus) string {
	switch status {
	case state.TaskStatusRateLimited, state.TaskStatusProviderUnavailable:
		return string(status)
	default:
		return ""
	}
}

func goalTerminalCompatible(status state.TaskStatus, pinned string, planKnown bool, action state.ParentAction) bool {
	if status != state.TaskStatusNone && status != state.TaskStatusComplete {
		return false
	}
	if status == state.TaskStatusNone && pinned != "" {
		return false
	}
	return planKnown && action == state.ParentActionNone
}

func projectContinuationFromPolicy(continuation repositoryproject.Continuation) projectContinuationObligation {
	return projectContinuationObligation{
		State:          continuation.State,
		Task:           continuation.Task,
		RequiredAction: continuation.RequiredAction,
		Reason:         continuation.Reason,
		Blocker:        cloneProjectBlocker(continuation.Blocker),
	}
}

func projectContinuationToPolicy(continuation projectContinuationObligation) repositoryproject.Continuation {
	return repositoryproject.Continuation{
		State:          continuation.State,
		Task:           continuation.Task,
		RequiredAction: continuation.RequiredAction,
		Reason:         continuation.Reason,
		Blocker:        cloneProjectBlocker(continuation.Blocker),
	}
}

func cloneProjectBlockers(blockers []projectStateBlocker) []repositoryproject.Blocker {
	cloned := make([]repositoryproject.Blocker, len(blockers))
	for i := range blockers {
		cloned[i] = blockers[i]
		cloned[i].Outstanding = append([]string(nil), blockers[i].Outstanding...)
	}
	return cloned
}

func cloneProjectBlocker(blocker *repositoryproject.Blocker) *projectStateBlocker {
	if blocker == nil {
		return nil
	}
	cloned := *blocker
	cloned.Outstanding = append([]string(nil), blocker.Outstanding...)
	return &cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func unknownProjectContinuation(reason string) projectContinuationObligation {
	return projectContinuationFromPolicy(repositoryproject.UnknownContinuation(reason))
}
