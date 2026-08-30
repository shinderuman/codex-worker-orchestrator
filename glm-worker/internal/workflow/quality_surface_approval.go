package workflow

import (
	"errors"
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) qualitySurfaceApprovalPending() bool {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	return err == nil && checkpoint.QualitySurfaceApprovalPending
}

func (w *Workflow) stopForQualitySurfaceApproval(checkpoint state.ResumeCheckpoint, result packet.Result) (bool, error) {
	changed, reason, err := w.inspectQualitySurfaceBaseline()
	if err != nil {
		return true, w.failClosedQualitySurface(checkpoint.Phase, reason, err)
	}
	if !changed {
		return false, nil
	}
	if err := w.validateCompletedGuardResult(result); err != nil {
		return true, err
	}

	checkpoint.QualitySurfaceApprovalPending = true
	checkpoint.CompletedResult = &result
	checkpoint.StopParentFiles = captureStopParentFiles(w.config.RepoRoot)
	if err := w.captureGuardRecoveryRetention(&checkpoint); err != nil {
		return true, err
	}
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return true, err
	}
	return true, w.failClosedQualitySurface(checkpoint.Phase, reason, nil)
}

func (w *Workflow) ExecuteQualitySurfaceApproval(acceptedScope string) error {
	return w.withTemp(func() error {
		if acceptedScope != acceptedFixScopeCurrentDiff {
			return &WorkerError{Message: "quality-surface approval requires accepted scope current-diff"}
		}
		if w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
			return &WorkerError{Message: "quality-surface approval is only available while waiting for Sol review"}
		}
		w.prepareAcceptedFixScope(acceptedScope)
		handled, err := w.resumeApprovedQualitySurface()
		if err != nil {
			return err
		}
		if !handled {
			return &WorkerError{Message: "no retained quality-surface approval checkpoint is available"}
		}
		return nil
	})
}

func (w *Workflow) resumeApprovedQualitySurface() (bool, error) {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !checkpoint.QualitySurfaceApprovalPending {
		return false, nil
	}
	if checkpoint.CompletedResult == nil {
		return true, &WorkerError{Phase: checkpoint.Phase, Message: "quality-surface approval checkpoint has no completed worker result"}
	}
	if err := w.validateCompletedGuardResult(*checkpoint.CompletedResult); err != nil {
		return true, err
	}
	if err := validateGuardRecoveryRetention(checkpoint); err != nil {
		return true, err
	}
	if err := w.verifyGuardRecoveryDirty(checkpoint); err != nil {
		return true, err
	}
	if err := w.verifyGuardRecoveryHead(checkpoint); err != nil {
		return true, err
	}
	if !w.acceptedFixScopeContainsCurrent() {
		return true, &WorkerError{Phase: checkpoint.Phase, Message: "current diff is not covered by the parent-approved quality-surface scope"}
	}
	stopped, err := w.verifyQualitySurfaceBaseline(checkpoint.Phase)
	if err != nil || stopped {
		return true, err
	}

	result := *checkpoint.CompletedResult
	checkpoint.QualitySurfaceApprovalPending = false
	checkpoint.CompletedResult = nil
	checkpoint.StopGitSnapshot = nil
	checkpoint.StopDirtyFiles = nil
	checkpoint.StopParentFiles = nil
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		return true, err
	}
	if err := w.state.SetTaskStatus(state.TaskStatusActive); err != nil {
		return true, err
	}

	activated := checkpointActivatedRules(checkpoint)
	result, err = w.convergeWorkerRuleActivation(checkpoint, result, activated)
	if err != nil {
		return true, err
	}
	switch checkpoint.Stage {
	case state.ResumeStageWorker:
		return true, w.handleWorkerResult(checkpoint.Request, result, checkpoint.Phase)
	case state.ResumeStageAutoFix:
		return true, w.handleAutoFixResult(
			checkpoint.Request,
			result,
			checkpoint.ReviewNumber,
			checkpoint.AutoFixes,
			checkpoint.Phase,
		)
	default:
		return true, &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("unsupported quality-surface approval stage: %s", checkpoint.Stage)}
	}
}

func (w *Workflow) discardPendingQualitySurfaceApproval() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	if !checkpoint.QualitySurfaceApprovalPending {
		return nil
	}
	return w.state.ClearResumeCheckpoint()
}
