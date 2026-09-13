package repositoryproject

type Continuation struct {
	State          string   `json:"state"`
	Task           string   `json:"task,omitempty"`
	RequiredAction string   `json:"required_action,omitempty"`
	Reason         string   `json:"reason"`
	Blocker        *Blocker `json:"blocker,omitempty"`
}

type ParentRequestCompletionProjection struct {
	CompletionAdmitted bool         `json:"completion_admitted"`
	StopAdmitted       bool         `json:"stop_admitted"`
	Continuation       Continuation `json:"continuation"`
}

type CompletionView struct {
	Ready bool
	Unmet []string
}

type ContinuationProjectView struct {
	PlanPresent  bool
	ProjectReady bool
	GoalPresent  bool
	GoalCompleted bool
	Active       []string
	NextRunnable *string
	Blockers     []Blocker
	Completion   *CompletionView
}

type ContinuationLifecycle struct {
	Interrupted           bool
	InterruptedResumeValid bool
	PinnedTask            string
	TaskAbsent            bool
	TaskComplete          bool
	ParentActionKnown     bool
	RequiredAction        string
	NoRequiredAction      bool
	TemporaryBlockReason  string
	GoalTerminalCompatible bool
}

const (
	ContinuationContinueNow                  = "continue-now"
	ContinuationBlocked                      = "blocked"
	ContinuationTerminal                     = "terminal"
	ContinuationExplicitStop                 = "explicit-stop"
	ContinuationDeferredByVerifiedAutomation = "deferred-by-verified-automation"
	ContinuationUnknown                      = "unknown"
)

const (
	ReasonPlanAbsent                  = "plan-absent"
	ReasonProjectStateIncomplete      = "project-state-incomplete"
	ReasonLifecycleInconsistent       = "lifecycle-inconsistent"
	ReasonUserInterruption            = "user-interruption"
	ReasonGoalCompleted               = "goal-completed"
	ReasonGoalLifecycleInconsistent   = "goal-lifecycle-inconsistent"
	ReasonActiveTaskUnresolved        = "active-task-unresolved"
	ReasonActiveTaskNotStarted        = "active-task-not-started"
	ReasonActiveTaskMismatch          = "active-task-mismatch"
	ReasonCurrentTask                 = "current-task"
	ReasonContinuationScopeUnbound    = "continuation-scope-unbound"
	ReasonNextRunnable                = "next-runnable"
	ReasonGoalAcceptancePending       = "goal-acceptance-pending"
	ReasonCompletionStateInconsistent = "completion-state-inconsistent"
	ReasonPostCompletionActive        = "post-local-completion-active"
	ActionStart                       = "start"
)

func DeriveContinuation(project ContinuationProjectView, lifecycle ContinuationLifecycle) Continuation {
	if lifecycle.Interrupted {
		return interruptedContinuation(project, lifecycle)
	}
	if !project.PlanPresent {
		return UnknownContinuation(ReasonPlanAbsent)
	}
	if !project.ProjectReady {
		return UnknownContinuation(ReasonProjectStateIncomplete)
	}
	if project.GoalPresent && project.GoalCompleted {
		if !lifecycle.GoalTerminalCompatible {
			return UnknownContinuation(ReasonGoalLifecycleInconsistent)
		}
		return Continuation{State: ContinuationTerminal, Reason: ReasonGoalCompleted}
	}
	if len(project.Active) != 1 {
		return UnknownContinuation(ReasonActiveTaskUnresolved)
	}
	return activeContinuation(project, lifecycle, project.Active[0])
}

func interruptedContinuation(project ContinuationProjectView, lifecycle ContinuationLifecycle) Continuation {
	if !lifecycle.InterruptedResumeValid {
		return UnknownContinuation(ReasonLifecycleInconsistent)
	}
	task := lifecycle.PinnedTask
	if task == "" && len(project.Active) == 1 {
		task = project.Active[0]
	}
	return Continuation{
		State:          ContinuationExplicitStop,
		Task:           task,
		RequiredAction: lifecycle.RequiredAction,
		Reason:         ReasonUserInterruption,
	}
}

func activeContinuation(project ContinuationProjectView, lifecycle ContinuationLifecycle, activeTask string) Continuation {
	if lifecycle.TaskAbsent {
		if lifecycle.PinnedTask != "" {
			return UnknownContinuation(ReasonActiveTaskMismatch)
		}
		return Continuation{State: ContinuationContinueNow, Task: activeTask, Reason: ReasonActiveTaskNotStarted}
	}
	if lifecycle.PinnedTask != activeTask {
		return UnknownContinuation(ReasonActiveTaskMismatch)
	}
	if !lifecycle.ParentActionKnown {
		return UnknownContinuation(ReasonLifecycleInconsistent)
	}
	if lifecycle.TemporaryBlockReason != "" {
		return Continuation{
			State:          ContinuationBlocked,
			Task:           activeTask,
			RequiredAction: lifecycle.RequiredAction,
			Reason:         lifecycle.TemporaryBlockReason,
		}
	}
	if !lifecycle.TaskComplete || !lifecycle.NoRequiredAction {
		return currentTaskContinuation(activeTask, lifecycle.RequiredAction, lifecycle.NoRequiredAction)
	}
	return completedActiveContinuation(project, activeTask)
}

func completedActiveContinuation(project ContinuationProjectView, activeTask string) Continuation {
	if !project.GoalPresent {
		return UnknownContinuation(ReasonContinuationScopeUnbound)
	}
	if project.NextRunnable != nil {
		return Continuation{State: ContinuationContinueNow, Task: *project.NextRunnable, Reason: ReasonNextRunnable}
	}
	if blocker := FirstBlocker(project.Blockers); blocker != nil {
		return Continuation{State: ContinuationBlocked, Task: blocker.Task, Reason: blocker.Reason, Blocker: blocker}
	}
	if project.Completion == nil {
		return UnknownContinuation(ReasonProjectStateIncomplete)
	}
	if project.Completion.Ready {
		return Continuation{State: ContinuationContinueNow, Task: activeTask, Reason: ReasonGoalAcceptancePending}
	}
	if len(project.Completion.Unmet) != 0 {
		return Continuation{State: ContinuationContinueNow, Task: activeTask, Reason: project.Completion.Unmet[0]}
	}
	return UnknownContinuation(ReasonCompletionStateInconsistent)
}

func currentTaskContinuation(task, requiredAction string, noRequiredAction bool) Continuation {
	continuation := Continuation{State: ContinuationContinueNow, Task: task, Reason: ReasonCurrentTask}
	if !noRequiredAction {
		continuation.RequiredAction = requiredAction
	}
	return continuation
}

func UnknownContinuation(reason string) Continuation {
	return Continuation{State: ContinuationUnknown, Reason: reason}
}

func FirstBlocker(blockers []Blocker) *Blocker {
	if len(blockers) == 0 {
		return nil
	}
	blocker := blockers[0]
	blocker.Outstanding = append([]string(nil), blocker.Outstanding...)
	return &blocker
}

func PostCompletionProjection(prepared PostCompletionPlan, graph *TaskGraph) ParentRequestCompletionProjection {
	switch prepared.Kind {
	case PostCompletionTerminal:
		return ParentRequestCompletionProjection{
			CompletionAdmitted: true,
			StopAdmitted:       true,
			Continuation:       Continuation{State: ContinuationTerminal, Reason: ReasonGoalCompleted},
		}
	case PostCompletionUnbound:
		return ParentRequestCompletionProjection{Continuation: UnknownContinuation(ReasonContinuationScopeUnbound)}
	}
	if len(prepared.Active) == 1 {
		return ParentRequestCompletionProjection{
			Continuation: Continuation{
				State:          ContinuationContinueNow,
				Task:           prepared.Active[0],
				RequiredAction: ActionStart,
				Reason:         ReasonPostCompletionActive,
			},
		}
	}
	if graph != nil {
		if runnable := graph.NextRunnable(prepared.Next); runnable != nil {
			return ParentRequestCompletionProjection{
				Continuation: Continuation{
					State:          ContinuationContinueNow,
					Task:           *runnable,
					RequiredAction: ActionStart,
					Reason:         ReasonNextRunnable,
				},
			}
		}
		if blocker := FirstBlocker(graph.Blockers(prepared.Next, prepared.Blocked)); blocker != nil {
			return ParentRequestCompletionProjection{
				StopAdmitted: true,
				Continuation: Continuation{
					State:   ContinuationBlocked,
					Task:    blocker.Task,
					Reason:  blocker.Reason,
					Blocker: blocker,
				},
			}
		}
	}
	return ParentRequestCompletionProjection{Continuation: UnknownContinuation(ReasonActiveTaskUnresolved)}
}
