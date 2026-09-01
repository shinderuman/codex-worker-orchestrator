package workflow

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) admitParentAction(action state.ParentAction) error {
	_, admitted, err := w.state.AdmitParentAction(action)
	if err != nil {
		return &WorkerError{Message: err.Error()}
	}
	if admitted {
		return nil
	}
	return w.parentActionDenied(action)
}

func (w *Workflow) admitNewTask() error {
	plan, admitted, err := w.state.AdmitNewTask()
	if err != nil {
		return &WorkerError{Message: err.Error()}
	}
	if admitted {
		return nil
	}
	return w.newTaskActionDenied(plan)
}

func (w *Workflow) parentActionDenied(action state.ParentAction) error {
	switch action {
	case state.ParentActionDecision:
		return &WorkerError{Message: "no pending Sol decision for this repository"}
	case state.ParentActionFix:
		if w.state.Exists("pending-decision") {
			return &WorkerError{Message: "task is waiting for Sol decision; resolve it before --fix"}
		}
		return &WorkerError{Message: "--fix is only available after NEEDS_SOL_REVIEW; start a new task after PASS"}
	case state.ParentActionResume:
		if _, err := w.state.LoadResumeCheckpoint(); err != nil {
			return err
		}
		return &WorkerError{Message: "saved task is not stopped by Z.ai 5h limit, provider unavailability, user interruption or a recoverable guard failure"}
	default:
		return &WorkerError{Message: fmt.Sprintf("parent action %s is not admitted", action)}
	}
}

func (w *Workflow) newTaskActionDenied(plan state.ParentActionPlan) error {
	switch plan.RequiredAction {
	case state.ParentActionDecision:
		return &WorkerError{Message: "previous task is waiting for Sol decision; use --decision or --reset"}
	case state.ParentActionReview, state.ParentActionAccept:
		label := w.state.OpenParentReviewLabel()
		if label == "none" {
			label = "NEEDS_SOL_REVIEW"
		}
		return &WorkerError{Message: fmt.Sprintf("previous task has unresolved parent review (%s); resolve it explicitly with --accept (or --fix when rework is required) before starting a new task", label)}
	case state.ParentActionResume:
		switch plan.ResumeKind {
		case "rate-limited":
			return &WorkerError{Message: "previous task is rate-limited; use --resume or --reset"}
		case "provider-unavailable":
			return &WorkerError{Message: "previous task is provider-unavailable; use --resume or --reset"}
		case "interrupted":
			return &WorkerError{Message: "previous task is interrupted; use --resume or --reset"}
		}
	case state.ParentActionRepairGuardThenResume:
		return &WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset"}
	}
	return &WorkerError{Message: fmt.Sprintf("previous task requires parent action %s before starting a new task", plan.RequiredAction)}
}
