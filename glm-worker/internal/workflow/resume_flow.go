package workflow

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ExecuteResume() error {
	return quietWhenParentFileGuardStopped(w.withTemp(w.executeResume))
}

func (w *Workflow) executeResume() error {
	if err := w.admitParentAction(state.ParentActionResume); err != nil {
		return err
	}
	checkpoint, decl, pocResume, err := w.loadResumeCheckpoint()
	if err != nil {
		return err
	}
	reuseCompletedResult, err := w.prepareStoppedResultReuse(checkpoint)
	if err != nil {
		return err
	}
	previousCheckpoint := checkpoint
	completedResult := checkpoint.CompletedResult
	stopKind := checkpoint.StopKind
	checkpoint, stopped, err := w.prepareResumeCheckpoint(checkpoint, decl, pocResume)
	if err != nil || stopped {
		return err
	}
	checkpoint.ClearStop()
	if reuseCompletedResult && completedResult != nil {
		if err := w.state.ClearResumeCheckpoint(); err != nil {
			return err
		}
		var routeErr error
		if stopKind == state.ResumeStopQualityGate {
			routeErr = w.resumeQualityGateResult(checkpoint, *completedResult)
		} else {
			routeErr = w.routeResumeResult(checkpoint, decl, *completedResult)
		}
		if routeErr != nil {
			return w.handleResumeRunError(checkpoint, previousCheckpoint, routeErr)
		}
		return nil
	}

	w.resetInstructionReadObservation()
	result, err := w.runModel(checkpoint)
	if err != nil {
		return w.handleResumeRunError(checkpoint, previousCheckpoint, err)
	}
	return w.routeResumeResult(checkpoint, decl, result)
}

func (w *Workflow) loadResumeCheckpoint() (state.ResumeCheckpoint, externalFeasibility, bool, error) {
	checkpoint, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, err
	}
	if !isKnownResumeStage(checkpoint.Stage) {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, &WorkerError{Message: fmt.Sprintf("unknown resume stage: %s", checkpoint.Stage)}
	}
	decl, err := w.gateExternalFeasibility(checkpoint.Phase, true)
	if err != nil {
		return state.ResumeCheckpoint{}, externalFeasibility{}, false, err
	}
	return checkpoint, decl, checkpoint.Stage == state.ResumeStageWorker && decl.pocStage(), nil
}

func (w *Workflow) prepareResumeCheckpoint(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	pocResume bool,
) (state.ResumeCheckpoint, bool, error) {
	if checkpoint.StopKind == state.ResumeStopInterrupted {
		if err := w.verifyInterruptedRetention(checkpoint); err != nil {
			return checkpoint, false, err
		}
	}
	if err := w.activateResume(checkpoint); err != nil {
		return checkpoint, false, err
	}
	if stopped, err := w.gateResumeSnapshots(checkpoint, pocResume); err != nil || stopped {
		return checkpoint, stopped, err
	}
	if err := w.gateResumeProvider(checkpoint); err != nil {
		return checkpoint, false, err
	}
	if checkpoint.Stage == state.ResumeStageReview {
		if stopped, err := w.verifyReviewResumeSnapshot(checkpoint); err != nil || stopped {
			return checkpoint, stopped, err
		}
	}
	checkpoint.Prompt = resumePrompt(checkpoint)
	activatedCheckpoint, activationErr := w.activateResumeRuleContext(checkpoint)
	if activationErr != nil {
		return checkpoint, false, activationErr
	}
	checkpoint = activatedCheckpoint
	if checkpoint.Stage == state.ResumeStageWorker {
		checkpoint.ReadOnly = resumeWorkerReadOnly(checkpoint, decl)
	}
	return checkpoint, false, nil
}

func resumeWorkerReadOnly(checkpoint state.ResumeCheckpoint, decl externalFeasibility) bool {
	if checkpoint.ResultCorrection {
		return true
	}
	return decl.pocStage()
}

func (w *Workflow) activateResumeRuleContext(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	if checkpoint.Role != state.WorkerRole || checkpoint.ReportOnly {
		return checkpoint, nil
	}
	activated, _, err := w.activateCheckpointRules(checkpoint)
	if err != nil {
		return checkpoint, err
	}
	return activated, nil
}

func (w *Workflow) gateResumeProvider(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.StopKind != state.ResumeStopProviderUnavailable {
		return nil
	}
	if err := w.gateResumeOnProbe(checkpoint); err != nil {
		return w.handleResumeProbeError(checkpoint, err)
	}
	return nil
}

func (w *Workflow) activateResume(checkpoint state.ResumeCheckpoint) error {
	attemptID := os.Getenv(state.GuardRepairResumeAttemptEnv)
	if attemptID != "" {
		if os.Getenv(state.GuardRepairParentActionEnv) != state.GuardRepairRebuiltResume {
			return fmt.Errorf("guard repair resume attempt is only valid for rebuilt resume")
		}
		if err := w.state.BeginResumeWithEvidence(checkpoint, attemptID); err != nil {
			return err
		}
	} else if err := w.state.BeginResume(checkpoint); err != nil {
		return err
	}
	w.currentResumeSource = checkpoint.StopKind.ResumeSource()
	return nil
}

func (w *Workflow) gateResumeSnapshots(checkpoint state.ResumeCheckpoint, pocResume bool) (bool, error) {
	if checkpoint.Stage == state.ResumeStageAutoFix && checkpoint.ReportOnly {
		if stopped, err := w.gateReportOnlyResumeSnapshot(); err != nil || stopped {
			return stopped, err
		}
	}
	if pocResume {
		if stopped, err := w.gatePoCResumeSnapshot(); err != nil || stopped {
			return stopped, err
		}
	}
	return false, nil
}

func (w *Workflow) handleResumeProbeError(checkpoint state.ResumeCheckpoint, err error) error {
	var interrupted *runner.InterruptedCallError
	if errors.As(err, &interrupted) {
		return w.interruptBetweenCalls(checkpoint)
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(err, &providerUnavailable) {
		return err
	}
	var limitErr runner.ZaiRateLimitError
	if errors.As(err, &limitErr) {
		return err
	}
	_ = w.state.ClearResumeCheckpoint()
	_ = w.state.RemoveUnreadySession(checkpoint.Role)
	return &WorkerError{Phase: checkpoint.Phase, Message: err.Error()}
}

func (w *Workflow) handleResumeRunError(_ state.ResumeCheckpoint, previous state.ResumeCheckpoint, runErr error) error {
	if isResumeStopError(runErr) {
		return runErr
	}
	if _, terminal := ResultCorrectionFailureFromError(runErr); terminal {
		return runErr
	}
	_ = w.attachStopRepositoryBoundary(&previous)
	_ = w.state.RestoreResumeStop(previous)
	return runErr
}

func isResumeStopError(err error) bool {
	if errors.Is(err, errParentFileGuardStopped) {
		return true
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(err, &interrupted) {
		return true
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(err, &providerUnavailable) {
		return true
	}
	var guardRecoverable *GuardRecoverableError
	if errors.As(err, &guardRecoverable) {
		return true
	}
	var limitErr runner.ZaiRateLimitError
	return errors.As(err, &limitErr)
}

func (w *Workflow) routeResumeResult(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	result packet.Result,
) error {
	switch checkpoint.Stage {
	case state.ResumeStageWorker:
		return w.routeWorkerResumeResult(checkpoint, decl, result)
	case state.ResumeStageReview:
		return w.routeReviewResumeResult(checkpoint, result)
	case state.ResumeStageAutoFix:
		return w.routeAutoFixResumeResult(checkpoint, result)
	default:
		return &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("unknown resume stage: %s", checkpoint.Stage)}
	}
}

func (w *Workflow) resumeQualityGateResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	if stopped, err := w.verifyQualitySurfaceBaseline(checkpoint.Phase); err != nil || stopped {
		return err
	}
	if err := w.state.ContinueAfterWorkerResult(); err != nil {
		return err
	}
	return w.reviewUntilStable(checkpoint.Request, result, checkpoint.ReviewNumber, checkpoint.AutoFixes, checkpoint.Phase)
}

func (w *Workflow) routeWorkerResumeResult(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	result packet.Result,
) error {
	if decl.pocStage() {
		if stopped, err := w.verifyPoCEndSnapshot(); err != nil || stopped {
			return err
		}
		if result.Status == packet.StatusImplemented {
			return w.routePoCWorkerResult(result)
		}
	}
	result, err := w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
	if err != nil {
		return err
	}
	return w.handleWorkerResult(checkpoint.Request, result, checkpoint.Phase)
}

func (w *Workflow) routeReviewResumeResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	if checkpoint.WorkerResult == nil {
		return &WorkerError{Phase: checkpoint.Phase, Message: "resume checkpoint has no worker result"}
	}
	if stopped, err := w.verifyReviewEndSnapshot(); err != nil || stopped {
		return err
	}
	workerResult := *checkpoint.WorkerResult
	reviewResult, stopped, err := w.resolveResumedReviewResult(checkpoint, workerResult, result)
	if err != nil || stopped {
		return err
	}
	if err := w.writeLastReview(reviewResult); err != nil {
		return err
	}
	return w.handleReviewResult(
		checkpoint.Request,
		workerResult,
		reviewResult,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
	)
}

func (w *Workflow) resolveResumedReviewResult(
	checkpoint state.ResumeCheckpoint,
	workerResult packet.Result,
	result packet.Result,
) (packet.Result, bool, error) {
	if checkpoint.RiskFloorReemit {
		return resolveRiskFloorReemit(result), false, nil
	}
	decision := w.state.ReadOr("last-decision", "none")
	highRiskFloor := w.resolveReviewResumeRisk(workerResult, checkpoint).high
	return w.enforceRiskFloor(
		checkpoint.Request,
		workerResult,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
		decision,
		highRiskFloor,
		result,
	)
}

func (w *Workflow) routeAutoFixResumeResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	if checkpoint.ReportOnly {
		if stopped, err := w.verifyReportOnlyEndSnapshot(); err != nil || stopped {
			return err
		}
	}
	if !checkpoint.ReportOnly {
		var err error
		result, err = w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
		if err != nil {
			return err
		}
	}
	return w.handleAutoFixResult(
		checkpoint.Request,
		result,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
		checkpoint.Phase,
	)
}

func isKnownResumeStage(stage state.ResumeStage) bool {
	switch stage {
	case state.ResumeStageWorker, state.ResumeStageReview, state.ResumeStageAutoFix:
		return true
	default:
		return false
	}
}
