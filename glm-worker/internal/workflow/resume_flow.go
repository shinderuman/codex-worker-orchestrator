package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type qualitySurfaceApprovalResumeMode uint8

const (
	qualitySurfaceApprovalResumeNone qualitySurfaceApprovalResumeMode = iota
	qualitySurfaceApprovalResumePending
	qualitySurfaceApprovalResumeActivated
)

func (w *Workflow) ExecuteResume() error {
	return quietWhenTerminalResultEmitted(w.withTemp(w.executeResume))
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
	if previousCheckpoint.RetainedReviewerCall != nil {
		return w.resumeRetainedReviewerCall(checkpoint, previousCheckpoint)
	}
	if reuseCompletedResult && completedResult != nil {
		if err := w.routeStoppedResultReuse(checkpoint, previousCheckpoint, stopKind, decl, completedResult); err != nil {
			return err
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

func (w *Workflow) routeStoppedResultReuse(
	checkpoint state.ResumeCheckpoint,
	previous state.ResumeCheckpoint,
	stopKind state.ResumeStopKind,
	decl externalFeasibility,
	completedResult *packet.Result,
) error {
	if previous.SummaryRecovered {
		if err := w.clearResultCorrectionRecord(); err != nil {
			return err
		}
		checkpoint.SummaryRecovered = false
	}
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
		return w.handleResumeRunError(checkpoint, previous, routeErr)
	}
	return nil
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
	approvalResumeMode, err := w.prepareQualitySurfaceApprovalResume(checkpoint)
	if err != nil {
		return checkpoint, false, err
	}
	if checkpoint.StopKind == state.ResumeStopInterrupted {
		if err := w.verifyInterruptedRetention(checkpoint); err != nil {
			return checkpoint, false, err
		}
	}
	if err := w.activateResume(checkpoint); err != nil {
		return checkpoint, false, err
	}
	if stopped, err := w.gateActivatedResumeCheckpoint(checkpoint, pocResume); err != nil || stopped {
		return checkpoint, stopped, err
	}
	if approvalResumeMode == qualitySurfaceApprovalResumePending {
		if err := w.restoreQualitySurfaceApprovalAfterResume(checkpoint); err != nil {
			return checkpoint, false, err
		}
		return checkpoint, true, nil
	}
	checkpoint, err = w.prepareResumeContinuation(checkpoint, decl, approvalResumeMode)
	if err != nil {
		return checkpoint, false, err
	}
	return checkpoint, false, nil
}

func (w *Workflow) prepareResumeContinuation(
	checkpoint state.ResumeCheckpoint,
	decl externalFeasibility,
	approvalResumeMode qualitySurfaceApprovalResumeMode,
) (state.ResumeCheckpoint, error) {
	if !checkpoint.StopKind.IsStopped() {
		return checkpoint, &WorkerError{Phase: checkpoint.Phase, Message: fmt.Sprintf("unsupported resume stop kind: %q", checkpoint.StopKind)}
	}
	prompt, err := w.resultCorrectionResumePrompt(checkpoint)
	if err != nil {
		return checkpoint, err
	}
	checkpoint.Prompt = prompt
	activatedCheckpoint, err := w.activateResumeRuleContext(checkpoint)
	if err != nil {
		return checkpoint, err
	}
	checkpoint = activatedCheckpoint
	checkpoint.ReadOnly = resumeCheckpointReadOnly(checkpoint, decl)
	if approvalResumeMode == qualitySurfaceApprovalResumeActivated {
		if err := checkpoint.ContinueActivatedQualitySurfaceApproval(); err != nil {
			return checkpoint, err
		}
	}
	return checkpoint, nil
}

func resumeCheckpointReadOnly(checkpoint state.ResumeCheckpoint, decl externalFeasibility) bool {
	if checkpoint.Stage != state.ResumeStageWorker {
		return checkpoint.ReadOnly
	}
	return resumeWorkerReadOnly(checkpoint, decl)
}

func (w *Workflow) gateActivatedResumeCheckpoint(
	checkpoint state.ResumeCheckpoint,
	pocResume bool,
) (bool, error) {
	if stopped, err := w.gateResumeSnapshots(checkpoint, pocResume); err != nil || stopped {
		return stopped, err
	}
	if err := w.gateResumeProvider(checkpoint); err != nil {
		return false, err
	}
	if checkpoint.Stage != state.ResumeStageReview {
		return false, nil
	}
	return w.verifyReviewResumeSnapshot(checkpoint)
}

func (w *Workflow) prepareQualitySurfaceApprovalResume(
	checkpoint state.ResumeCheckpoint,
) (qualitySurfaceApprovalResumeMode, error) {
	if !checkpoint.QualitySurfaceApprovalPending || !checkpoint.IsStopped() {
		return qualitySurfaceApprovalResumeNone, nil
	}
	if !checkpoint.StopKind.CanInterruptQualitySurfaceApproval() {
		return qualitySurfaceApprovalResumeNone, fmt.Errorf("quality-surface approval cannot resume from stop kind %q", checkpoint.StopKind)
	}
	if checkpoint.CompletedResult == nil {
		return qualitySurfaceApprovalResumeNone, &WorkerError{Phase: checkpoint.Phase, Message: "stopped quality-surface approval checkpoint has no completed worker result"}
	}
	if err := w.validateApprovedQualitySurfaceRetention(checkpoint); err != nil {
		return qualitySurfaceApprovalResumeNone, err
	}
	if checkpoint.QualitySurfaceApprovalActivated {
		return qualitySurfaceApprovalResumeActivated, nil
	}
	return qualitySurfaceApprovalResumePending, nil
}

func (w *Workflow) restoreQualitySurfaceApprovalAfterResume(checkpoint state.ResumeCheckpoint) error {
	if checkpoint.QualitySurfaceApprovalActivated {
		return fmt.Errorf("activated quality-surface approval continuation cannot restore the pending approval wait")
	}
	restored := checkpoint
	restored.ClearStop()
	if err := w.captureStopRetention(&restored); err != nil {
		return w.restoreResumeStop(checkpoint, fmt.Errorf("refresh quality-surface approval retention: %w", err))
	}
	if err := w.state.EnterQualitySurfaceApprovalWait(restored); err != nil {
		return w.restoreResumeStop(checkpoint, fmt.Errorf("restore quality-surface approval wait: %w", err))
	}
	return nil
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
	if err := w.state.BeginResume(checkpoint); err != nil {
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
	failure := &WorkerError{Phase: checkpoint.Phase, Message: err.Error()}
	return w.restoreResumeStop(checkpoint, failure)
}

func (w *Workflow) handleResumeRunError(_ state.ResumeCheckpoint, previous state.ResumeCheckpoint, runErr error) error {
	if isResumeStopError(runErr) {
		return runErr
	}
	if _, terminal := ResultCorrectionFailureFromError(runErr); terminal {
		return runErr
	}
	rollback := previous
	if err := w.attachStopRepositoryBoundary(&rollback); err != nil {
		failure := errors.Join(runErr, fmt.Errorf("capture resume rollback boundary: %w", err))
		return w.restoreResumeStop(previous, failure)
	}
	return w.restoreResumeStop(rollback, runErr)
}

func (w *Workflow) restoreResumeStop(previous state.ResumeCheckpoint, cause error) error {
	if err := w.state.RestoreResumeStop(previous); err != nil {
		return errors.Join(cause, fmt.Errorf("restore previous resume stop: %w", err))
	}
	return cause
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
	checkpoint = acceptedResultContinuationCheckpoint(checkpoint, decl)
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

func acceptedResultContinuationCheckpoint(checkpoint state.ResumeCheckpoint, decl externalFeasibility) state.ResumeCheckpoint {
	if !checkpoint.ResultCorrection {
		return checkpoint
	}
	checkpoint.ResultCorrection = false
	switch checkpoint.Stage {
	case state.ResumeStageWorker:
		checkpoint.ReadOnly = checkpoint.ReportOnly || resumeWorkerReadOnly(checkpoint, decl)
	case state.ResumeStageAutoFix:
		checkpoint.ReadOnly = checkpoint.ReportOnly || decl.pocStage()
	}
	return checkpoint
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
	risk := w.resolveReviewResumeRisk(*checkpoint.WorkerResult, checkpoint)
	return w.finishReviewResult(checkpoint, result, risk.high)
}

func (w *Workflow) finishReviewResult(checkpoint state.ResumeCheckpoint, result packet.Result, highRisk bool) error {
	workerResult := *checkpoint.WorkerResult
	accepted, err := w.state.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	var reviewResult packet.Result
	if accepted.RetainedReviewerCall != nil {
		reviewResult, err = w.riskFloorEscalationResult(result)
	} else {
		reviewResult, err = w.enforceRiskFloor(highRisk, result)
	}
	if err != nil {
		return w.retainReviewerCall(accepted, err)
	}
	if err := w.writeLastReview(reviewResult); err != nil {
		if result.Status == packet.StatusPass && reviewResult.Status == packet.StatusNeedsSolReview {
			cleanupErr := os.Remove(reviewResult.Artifacts[0])
			return w.retainReviewerCall(accepted, errors.Join(err, cleanupErr))
		}
		return err
	}
	if err := w.clearAcceptedModelCheckpoint(accepted); err != nil {
		return err
	}
	reviewResult = w.attachFailurePathAdvisory(checkpoint.Request, reviewResult, checkpoint.ReviewNumber)
	return w.handleReviewResult(
		checkpoint.Request,
		workerResult,
		reviewResult,
		checkpoint.ReviewNumber,
		checkpoint.AutoFixes,
	)
}

func (w *Workflow) verifyRetainedReviewerCall(checkpoint state.ResumeCheckpoint) error {
	retained := checkpoint.RetainedReviewerCall
	if retained.TaskID != w.state.ReadOr("task.id", "") || retained.SessionID != w.state.ReadOr("reviewer.id", "") {
		return fmt.Errorf("retained reviewer call no longer matches the task/session")
	}
	logs, err := w.state.ReadModelCallLogs(retained.TaskID)
	if err != nil {
		return err
	}
	call := latestAcceptedResultTaskCall(logs)
	if !retainedReviewerLogMatches(call, checkpoint) ||
		call.StructuredOutputSHA256 != resultRecoveryDigest(retained.RawPacket) {
		return fmt.Errorf("retained reviewer call differs from the latest accepted Reviewer log")
	}
	if checkpoint.ResultCorrection != strings.HasSuffix(call.Phase, resultCorrectionPhaseSuffix) {
		return fmt.Errorf("retained reviewer call differs from the result correction phase")
	}
	return w.validateResultCorrectionBoundary(checkpoint)
}

func retainedReviewerLogMatches(call state.ModelCallLog, checkpoint state.ResumeCheckpoint) bool {
	retained := checkpoint.RetainedReviewerCall
	return call.CallID == retained.CallID && call.TaskID == retained.TaskID && call.SessionID == retained.SessionID &&
		call.Role == state.ReviewerRole && call.Phase == checkpoint.Phase && call.ModelAlias == checkpoint.Model &&
		call.Outcome == acceptedResultCallOutcome && call.ReadOnly && call.PacketStatus == string(packet.StatusPass)
}

func (w *Workflow) retainReviewerCall(checkpoint state.ResumeCheckpoint, cause error) error {
	call := checkpoint.RetainedReviewerCall
	if call == nil {
		call = &state.RetainedReviewerCall{
			CallID: w.lastCallID, TaskID: w.state.ReadOr("task.id", ""),
			SessionID: w.state.ReadOr("reviewer.id", ""), RawPacket: w.lastReviewerRawPacket,
		}
	}
	checkpoint.SetStopKind(state.ResumeStopGuardRecoverable)
	checkpoint.GuardFailure = boundedText(cause.Error(), packet.MaxDiagnosticBytes)
	checkpoint.RetainedReviewerCall = call
	if err := w.captureStopRetention(&checkpoint); err != nil {
		return errors.Join(cause, err)
	}
	if err := w.state.EnterStop(checkpoint); err != nil {
		return errors.Join(cause, err)
	}
	return w.guardRecoverableError(checkpoint)
}

func (w *Workflow) resumeRetainedReviewerCall(checkpoint, previous state.ResumeCheckpoint) error {
	call := previous.RetainedReviewerCall
	result, err := w.parseModelCallResult(checkpoint, runner.RunResult{StructuredOutput: call.RawPacket})
	if err != nil {
		return w.restoreResumeStop(previous, fmt.Errorf("validate retained reviewer call: %w", err))
	}
	if result.Status != packet.StatusPass {
		return w.restoreResumeStop(previous, fmt.Errorf("retained reviewer call has status %s, expected PASS", result.Status))
	}
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(call.TaskID)); err != nil {
		return w.restoreResumeStop(previous, err)
	}
	w.lastCallID = call.CallID
	w.lastReviewerRawPacket = call.RawPacket
	w.lastProducer = state.ParentReviewProducer{Role: string(checkpoint.Role), Model: checkpoint.Model}
	return w.routeReviewResumeResult(checkpoint, result)
}

func (w *Workflow) routeAutoFixResumeResult(checkpoint state.ResumeCheckpoint, result packet.Result) error {
	var err error
	if checkpoint.ReportOnly {
		if stopped, err := w.verifyReportOnlyEndSnapshot(); err != nil || stopped {
			return err
		}
		result, err = w.convergeParentValidation(result)
	} else {
		result, err = w.convergeWorkerRuleActivation(checkpoint, result, w.activatedRulesForCheckpoint(checkpoint))
	}
	if err != nil {
		return err
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
