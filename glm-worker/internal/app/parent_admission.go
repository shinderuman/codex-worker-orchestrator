package app

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func admitParentCommand(cmd Command, st *state.StateStore) error {
	if cmd.Mode == ModeNewTask {
		resume, err := st.AdmitNewTaskRotation(os.Getenv(state.ParentActionCodexThreadIDEnv), os.Getenv(state.SessionRotationClaimIDEnv))
		if err != nil {
			return &workflow.WorkerError{Message: err.Error()}
		}
		if resume {
			return nil
		}
		plan, admitted, err := st.AdmitNewTask()
		if err != nil {
			return &workflow.WorkerError{Message: err.Error()}
		}
		if admitted {
			return nil
		}
		return parentActionDenied(cmd, plan, st)
	}

	action, parentCommand := commandParentAction(cmd.Mode)
	if !parentCommand {
		return nil
	}
	plan, admitted, err := st.AdmitParentAction(action)
	if err != nil {
		return &workflow.WorkerError{Message: err.Error()}
	}
	if admitted {
		return nil
	}
	return parentActionDenied(cmd, plan, st)
}

func commandParentAction(mode CommandMode) (state.ParentAction, bool) {
	switch mode {
	case ModeDecision:
		return state.ParentActionDecision, true
	case ModeFix:
		return state.ParentActionFix, true
	case ModeApproveSurface:
		return state.ParentActionApproveSurface, true
	case ModeAccept:
		return state.ParentActionAccept, true
	case ModeResume:
		return state.ParentActionResume, true
	case ModePark:
		return state.ParentActionPark, true
	case ModeUnpark:
		return state.ParentActionUnpark, true
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
	case ModeApproveSurface:
		return &workflow.WorkerError{Message: "quality-surface approval is not pending; --approve-surface requires a stopped quality policy surface change"}
	case ModeAccept:
		if plan.RequiredAction == state.ParentActionApproveSurface {
			return &workflow.WorkerError{Message: "task is waiting for quality policy surface approval; resolve it with --approve-surface current-diff (or --fix) before --accept"}
		}
		if plan.RequiredAction == state.ParentActionComplete {
			return &workflow.WorkerError{Message: "task is awaiting parent completion; run the parent push and glm-parent-action complete before starting further actions"}
		}
		return &workflow.WorkerError{Message: "pending Sol decision must be resolved with --decision before --accept"}
	case ModeResume:
		return resumeActionDenied(st)
	case ModePark, ModeUnpark:
		return parkActionDenied(cmd, plan)
	case ModeNewTask:
		return newTaskActionDenied(plan, st)
	default:
		return &workflow.WorkerError{Message: fmt.Sprintf("parent action %d is not admitted", cmd.Mode)}
	}
}

func parkActionDenied(cmd Command, plan state.ParentActionPlan) error {
	if cmd.Mode == ModeUnpark {
		return &workflow.WorkerError{Message: "no parked task is available to unpark in this repository"}
	}
	if plan.RequiredAction == state.ParentActionUnpark {
		return &workflow.WorkerError{Message: "task is already parked; run the interrupt task in the parked worktree or unpark before parking again"}
	}
	return &workflow.WorkerError{Message: "park is only available while the task waits for a Sol decision or Sol review; use --decision, --fix, --approve-surface or --accept instead"}
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
	case state.ParentActionApproveSurface:
		return &workflow.WorkerError{Message: "previous task is waiting for quality policy surface approval; resolve it with glm-parent-action approve-surface --accepted-scope current-diff (or --fix) before starting a new task"}
	case state.ParentActionComplete:
		return &workflow.WorkerError{Message: "previous task is awaiting parent completion; finish the parent push and run glm-parent-action complete before starting a new task"}
	case state.ParentActionResume:
		if message, ok := resumeDeniedMessage(plan.ResumeKind); ok {
			return &workflow.WorkerError{Message: message}
		}
	case state.ParentActionRepairGuardThenResume:
		return &workflow.WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume or --reset"}
	case state.ParentActionRepairQualityGateThenResume:
		return &workflow.WorkerError{Message: "previous task stopped on a deterministic quality gate failure; repair the reported gate precondition then use --resume or --reset"}
	case state.ParentActionUnpark:
		return &workflow.WorkerError{Message: "previous task is parked for an interrupt task; unpark it (after integrating the interrupt work) or run the interrupt task inside the parked worktree"}
	}
	return &workflow.WorkerError{Message: fmt.Sprintf("previous task requires parent action %s before starting a new task", plan.RequiredAction)}
}

func resumeDeniedMessage(resumeKind string) (string, bool) {
	switch resumeKind {
	case "rate-limited":
		return "previous task is rate-limited; use --resume or --reset", true
	case "provider-unavailable":
		return "previous task is provider-unavailable; use --resume or --reset", true
	case "interrupted":
		return "previous task is interrupted; use --resume or --reset", true
	}
	return "", false
}
