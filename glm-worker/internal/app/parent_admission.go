package app

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func admitParentCommand(cmd Command, st *state.StateStore) error {
	if cmd.Mode != ModeNewTask {
		action, parentCommand := commandParentAction(cmd.Mode)
		if !parentCommand {
			return nil
		}
		plan, err := parentActionPlan(st)
		if err != nil {
			return err
		}
		if plan.AdmitsCommand(action) {
			return nil
		}
		return parentActionDenied(cmd, plan, st)
	}

	plan, err := parentActionPlan(st)
	if err != nil {
		return err
	}
	if plan.RequiredAction == state.ParentActionNone {
		return nil
	}
	return parentActionDenied(cmd, plan, st)
}

func parentActionPlan(st *state.StateStore) (state.ParentActionPlan, error) {
	plan, err := st.ParentActionPlan()
	if err != nil {
		return state.ParentActionPlan{}, &workflow.WorkerError{Message: err.Error()}
	}
	return plan, nil
}

func commandParentAction(mode CommandMode) (state.ParentAction, bool) {
	switch mode {
	case ModeDecision:
		return state.ParentActionDecision, true
	case ModeFix:
		return state.ParentActionFix, true
	case ModeAccept:
		return state.ParentActionAccept, true
	case ModeResume:
		return state.ParentActionResume, true
	default:
		return state.ParentActionNone, false
	}
}

func parentActionDenied(cmd Command, plan state.ParentActionPlan, st *state.StateStore) error {
	switch cmd.Mode {
	case ModeDecision:
		return &workflow.WorkerError{Message: "no pending Sol decision for this repository"}
	case ModeFix:
		if st.Exists("pending-decision") {
			return &workflow.WorkerError{Message: "task is waiting for Sol decision; resolve it before --fix"}
		}
		return &workflow.WorkerError{Message: "--fix is only available after NEEDS_SOL_REVIEW; start a new task after PASS"}
	case ModeAccept:
		return &workflow.WorkerError{Message: "pending Sol decision must be resolved with --decision before --accept"}
	case ModeResume:
		return resumeActionDenied(st)
	case ModeNewTask:
		return newTaskActionDenied(plan, st)
	default:
		return &workflow.WorkerError{Message: fmt.Sprintf("parent action %d is not admitted", cmd.Mode)}
	}
}

func resumeActionDenied(st *state.StateStore) error {
	if _, err := st.LoadResumeCheckpoint(); err != nil {
		return err
	}
	return &workflow.WorkerError{Message: "saved task is not stopped by Z.ai 5h limit, provider unavailability, user interruption or a recoverable guard failure"}
}

func newTaskActionDenied(plan state.ParentActionPlan, st *state.StateStore) error {
	switch plan.RequiredAction {
	case state.ParentActionDecision:
		return &workflow.WorkerError{Message: "previous task is waiting for Sol decision; use --decision or --reset"}
	case state.ParentActionReview, state.ParentActionAccept:
		label := st.OpenParentReviewLabel()
		if label == "none" {
			label = "NEEDS_SOL_REVIEW"
		}
		return &workflow.WorkerError{Message: fmt.Sprintf("previous task has unresolved parent review (%s); resolve it explicitly with --accept (or --fix when rework is required) before starting a new task", label)}
	case state.ParentActionResume:
		switch plan.ResumeKind {
		case "rate-limited":
			return &workflow.WorkerError{Message: "previous task is rate-limited; use --resume or --reset"}
		case "provider-unavailable":
			return &workflow.WorkerError{Message: "previous task is provider-unavailable; use --resume or --reset"}
		case "interrupted":
			return &workflow.WorkerError{Message: "previous task is interrupted; use --resume or --reset"}
		}
	case state.ParentActionRepairGuardThenResume:
		return &workflow.WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset"}
	}
	return &workflow.WorkerError{Message: fmt.Sprintf("previous task requires parent action %s before starting a new task", plan.RequiredAction)}
}
