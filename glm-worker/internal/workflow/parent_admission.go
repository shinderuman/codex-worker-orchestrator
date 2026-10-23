package workflow

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) admitParentAction(action state.ParentAction) error {
	plan, err := w.state.ParentActionPlan()
	if err != nil {
		return &WorkerError{Message: err.Error()}
	}
	if plan.AdmitsCommand(action) {
		return w.admitCanonicalMutation()
	}
	return w.parentActionDenied(action)
}

func (w *Workflow) admitNewTask() error {
	if err := w.state.ClearPendingSessionRotationRecommendationRetirement(); err != nil {
		return &WorkerError{Message: err.Error()}
	}
	claimID := os.Getenv(state.SessionRotationClaimIDEnv)
	threadID := os.Getenv(state.ParentActionCodexThreadIDEnv)
	resume, err := w.state.AdmitNewTaskRotationBoundary(threadID, claimID)
	if err != nil {
		return &WorkerError{Message: err.Error()}
	}
	if resume {
		return w.admitCanonicalMutation()
	}
	plan, admitted, err := w.state.AdmitNewTask()
	if err != nil {
		return &WorkerError{Message: err.Error()}
	}
	if admitted {
		if err := w.admitNewTaskMutation(); err != nil {
			return err
		}
		if claimID == "" {
			if err := w.state.StagePendingSessionRotationRecommendationRetirement(threadID); err != nil {
				return &WorkerError{Message: err.Error()}
			}
		}
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
	case state.ParentActionApproveSurface:
		return &WorkerError{Message: "quality-surface approval is not pending; --approve-surface requires a stopped quality policy surface change"}
	case state.ParentActionResume:
		checkpoint, err := w.state.LoadResumeCheckpoint()
		if err != nil {
			return err
		}
		if checkpoint.StopKind == state.ResumeStopResultFormat {
			return &WorkerError{Message: "terminal result correction needs the one-shot report resubmission; use glm-parent-action resubmit-report instead of --resume"}
		}
		return &WorkerError{Message: "saved task is not stopped by Z.ai 5h limit, provider unavailability, user interruption or a recoverable guard failure"}
	default:
		return &WorkerError{Message: fmt.Sprintf("parent action %s is not admitted", action)}
	}
}

func (w *Workflow) newTaskActionDenied(plan state.ParentActionPlan) error {
	switch plan.RequiredAction {
	case state.ParentActionDecision:
		return &WorkerError{Message: "previous task is waiting for Sol decision; use --decision"}
	case state.ParentActionReview, state.ParentActionAccept:
		label := w.state.OpenParentReviewLabel()
		if label == "none" {
			label = "NEEDS_SOL_REVIEW"
		}
		return &WorkerError{Message: fmt.Sprintf("previous task has unresolved parent review (%s); resolve it explicitly with --accept (or --fix when rework is required) before starting a new task", label)}
	case state.ParentActionApproveSurface:
		return &WorkerError{Message: "previous task is waiting for quality policy surface approval; resolve it with glm-parent-action approve-surface --accepted-scope current-diff (or --fix) before starting a new task"}
	case state.ParentActionComplete:
		return &WorkerError{Message: "previous task is awaiting parent completion; finish canonical controller publication and retirement before starting a new task"}
	case state.ParentActionResume:
		if message := resumeDeniedMessage(plan.ResumeKind); message != "" {
			return &WorkerError{Message: message}
		}
	case state.ParentActionRepairGuardThenResume:
		return &WorkerError{Message: "previous task stopped on a recoverable guard failure; repair the guard then use --resume"}
	case state.ParentActionRepairQualityGateThenResume:
		return &WorkerError{Message: "previous task stopped on a deterministic quality gate failure; repair the reported gate precondition then use --resume"}
	case state.ParentActionResubmitReport:
		return &WorkerError{Message: "previous task stopped on a terminal result correction; use glm-parent-action resubmit-report before starting a new task"}
	}
	return &WorkerError{Message: fmt.Sprintf("previous task requires parent action %s before starting a new task", plan.RequiredAction)}
}

func resumeDeniedMessage(kind string) string {
	switch kind {
	case "rate-limited":
		return "previous task is rate-limited; use --resume"
	case "provider-unavailable":
		return "previous task is provider-unavailable; use --resume"
	case "interrupted":
		return "previous task is interrupted; use --resume"
	}
	return ""
}
