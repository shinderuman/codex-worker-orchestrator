package workflow

import (
	"errors"
	"fmt"
	"strings"

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
	if err := w.captureStopRetention(&checkpoint); err != nil {
		return true, err
	}
	if err := w.state.EnterQualitySurfaceApprovalWait(checkpoint); err != nil {
		return true, err
	}
	return true, w.emitResult(qualitySurfaceFailClosedResult(checkpoint.Phase, reason))
}

func (w *Workflow) ExecuteQualitySurfaceApproval(acceptedScope string) error {
	return w.withTemp(func() error {
		if err := w.admitParentAction(state.ParentActionApproveSurface); err != nil {
			return err
		}
		if acceptedScope != acceptedFixScopeCurrentDiff {
			return &WorkerError{Message: "quality-surface approval requires accepted scope current-diff"}
		}
		if w.state.TaskStatus() != state.TaskStatusWaitingSolReview {
			return &WorkerError{Message: "quality-surface approval is only available while waiting for Sol review"}
		}
		if err := w.prepareAcceptedFixScopeForAction(acceptedScope, state.ParentActionApproveSurface); err != nil {
			return err
		}
		handled, err := w.resumeApprovedQualitySurface()
		if err != nil {
			return err
		}
		if !handled {
			return w.discardAcceptedFixScopeAfterFailure(&WorkerError{Message: "no retained quality-surface approval checkpoint is available"})
		}
		return nil
	})
}

func (w *Workflow) resumeApprovedQualitySurface() (bool, error) {
	checkpoint, handled, err := w.loadApprovedQualitySurfaceCheckpoint()
	if err != nil || !handled {
		return handled, err
	}
	result := *checkpoint.CompletedResult
	if err := w.activateApprovedQualitySurface(); err != nil {
		return true, err
	}
	result, err = w.convergeWorkerRuleActivation(checkpoint, result, checkpointActivatedRules(checkpoint))
	if err != nil {
		return true, err
	}
	return true, w.routeApprovedQualitySurface(checkpoint, result)
}

func (w *Workflow) loadApprovedQualitySurfaceCheckpoint() (state.ResumeCheckpoint, bool, error) {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) {
		if scopeErr := w.discardPreparedAcceptedFixScope(); scopeErr != nil {
			return state.ResumeCheckpoint{}, false, scopeErr
		}
		return state.ResumeCheckpoint{}, false, nil
	}
	if err != nil {
		return state.ResumeCheckpoint{}, false, w.discardAcceptedFixScopeAfterFailure(err)
	}
	if !checkpoint.QualitySurfaceApprovalPending {
		if scopeErr := w.discardPreparedAcceptedFixScope(); scopeErr != nil {
			return state.ResumeCheckpoint{}, false, scopeErr
		}
		return state.ResumeCheckpoint{}, false, nil
	}
	if checkpoint.CompletedResult == nil {
		return checkpoint, true, w.discardAcceptedFixScopeAfterFailure(&WorkerError{Phase: checkpoint.Phase, Message: "quality-surface approval checkpoint has no completed worker result"})
	}
	if err := w.validateApprovedQualitySurfaceRetention(checkpoint); err != nil {
		return checkpoint, true, w.discardAcceptedFixScopeAfterFailure(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		return checkpoint, true, w.discardAcceptedFixScopeAfterFailure(&WorkerError{Phase: checkpoint.Phase, Message: "current diff is not covered by the parent-approved quality-surface scope"})
	}
	return checkpoint, true, nil
}

func (w *Workflow) validateApprovedQualitySurfaceRetention(checkpoint state.ResumeCheckpoint) error {
	if err := w.validateCompletedGuardResult(*checkpoint.CompletedResult); err != nil {
		return err
	}
	if err := validateGuardRecoveryRetention(checkpoint); err != nil {
		return err
	}
	if err := w.verifyGuardRecoveryDirty(checkpoint); err != nil {
		return err
	}
	return w.verifyGuardRecoveryHead(checkpoint)
}

func (w *Workflow) activateApprovedQualitySurface() error {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return w.discardAcceptedFixScopeAfterFailure(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		return w.discardAcceptedFixScopeAfterFailure(&WorkerError{Phase: checkpoint.Phase, Message: "current diff is not covered by the parent-approved quality-surface scope"})
	}
	previousBaseline, approvedBaseline, advance, err := w.prepareApprovedQualitySurfaceBaseline(checkpoint.Phase)
	if err != nil {
		return w.discardAcceptedFixScopeAfterFailure(err)
	}
	if advance {
		if err := w.state.Write(qualitySurfaceBaselineStateKey, approvedBaseline); err != nil {
			return w.discardAcceptedFixScopeAfterFailure(fmt.Errorf("persist approved quality-surface baseline: %w", err))
		}
	}
	if err := w.state.ActivateQualitySurfaceApproval(); err != nil {
		if advance {
			if rollbackErr := w.state.Write(qualitySurfaceBaselineStateKey, previousBaseline); rollbackErr != nil {
				err = fmt.Errorf("quality-surface approval activation failed and baseline rollback failed: activation=%w rollback=%w", err, rollbackErr)
			}
		}
		return w.discardAcceptedFixScopeAfterFailure(err)
	}
	return nil
}

func (w *Workflow) prepareApprovedQualitySurfaceBaseline(phase string) (string, string, bool, error) {
	current, err := w.captureQualitySurface(w.config.RepoRoot)
	if err != nil {
		return "", "", false, w.approvedQualitySurfaceValidationFailure(phase, "quality policy surfaceを再計測できません", err)
	}
	if current == "" {
		return "", "", false, nil
	}
	if !w.state.Exists(qualitySurfaceBaselineStateKey) {
		return "", "", false, w.approvedQualitySurfaceValidationFailure(
			phase,
			"worker開始時のquality policy baselineがありません",
			fmt.Errorf("required state %s is missing", qualitySurfaceBaselineStateKey),
		)
	}
	baseline, err := w.state.Read(qualitySurfaceBaselineStateKey)
	if err != nil {
		return "", "", false, w.approvedQualitySurfaceValidationFailure(phase, "worker開始時のquality policy baselineを読めません", err)
	}
	if strings.TrimSpace(baseline) == current {
		return baseline, current, false, nil
	}
	if !w.acceptedFixScopeContainsCurrent() {
		return "", "", false, w.approvedQualitySurfaceValidationFailure(phase, "workerがquality policy surfaceを変更しました", nil)
	}
	return baseline, current, true, nil
}

func (w *Workflow) approvedQualitySurfaceValidationFailure(phase, reason string, cause error) error {
	if err := w.failClosedQualitySurface(phase, reason, cause); err != nil {
		return err
	}
	return &WorkerError{Phase: phase, Message: "quality-surface approval validation stopped before activation"}
}

func (w *Workflow) routeApprovedQualitySurface(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	switch checkpoint.Stage {
	case state.ResumeStageWorker:
		return w.handleWorkerResult(checkpoint.Request, result, checkpoint.Phase)
	case state.ResumeStageAutoFix:
		return w.handleAutoFixResult(
			checkpoint.Request,
			result,
			checkpoint.ReviewNumber,
			checkpoint.AutoFixes,
			checkpoint.Phase,
		)
	default:
		return &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("unsupported quality-surface approval stage: %s", checkpoint.Stage)}
	}
}
