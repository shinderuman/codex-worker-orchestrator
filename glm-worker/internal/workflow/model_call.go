package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) runModel(checkpoint state.ResumeCheckpoint) (packet.Result, error) {
	checkpoint, outputPath, guardBefore, err := w.prepareModelCall(checkpoint)
	if err != nil {
		return packet.Result{}, err
	}
	execution, err := w.invokeModelCall(checkpoint, outputPath, guardBefore)
	if err != nil {
		return packet.Result{}, err
	}
	execution, err = w.resolveModelCallFailure(checkpoint, outputPath, execution)
	if err != nil {
		return packet.Result{}, err
	}
	w.observeInstructionReads(execution.runResult.InstructionReads)
	if err := w.finalizeModelCallState(checkpoint, outputPath, execution); err != nil {
		return packet.Result{}, err
	}
	if checkpoint.ResultCorrection {
		if err := w.validateResultCorrectionBoundary(checkpoint); err != nil {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "invalid_packet", "", err, outputPath, callDiagnostics{})
			return packet.Result{}, err
		}
	}

	result, err := w.parseModelCallResult(checkpoint, execution.runResult)
	if err != nil {
		return w.handleInvalidModelResult(checkpoint, outputPath, execution, err)
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return packet.Result{}, err
	}
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(taskID)); err != nil {
		return w.handleInvalidModelResult(checkpoint, outputPath, execution, err)
	}
	if checkpoint.ResultCorrection {
		if err := w.clearResultCorrectionRecord(); err != nil {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
			return packet.Result{}, err
		}
	}
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "success", string(result.Status), nil, outputPath, callDiagnostics{reportedRisk: string(result.Risk)})
	w.lastProducer = state.ParentReviewProducer{Role: string(checkpoint.Role), Model: checkpoint.Model}
	return result, nil
}

func (w *Workflow) prepareModelCall(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, string, parentFileGuard, error) {
	outputPath := filepath.Join(w.temp, checkpoint.Phase+".log")
	if err := w.validateResultCorrectionBoundary(checkpoint); err != nil {
		return checkpoint, outputPath, parentFileGuard{}, err
	}
	if err := w.applyModelArtifactContext(&checkpoint); err != nil {
		return checkpoint, outputPath, parentFileGuard{}, err
	}
	if checkpoint.OriginalPrompt == "" {
		checkpoint.OriginalPrompt = checkpoint.Prompt
	}
	if checkpoint.Model == "" {
		return checkpoint, outputPath, parentFileGuard{}, &WorkerError{Phase: checkpoint.Phase, Message: "checkpoint model is missing"}
	}
	if checkpoint.Effort == "" {
		checkpoint.Effort = w.config.RoutineEffort
	}

	guardBefore, stopped, err := w.captureParentFileGuard(checkpoint.Role)
	if stopped {
		return checkpoint, outputPath, guardBefore, err
	}
	if checkpoint.Stage == state.ResumeStageReview && checkpoint.StopGitSnapshot != nil {
		stopSnapshot := *checkpoint.StopGitSnapshot
		stopSnapshot.ParentFiles = nil
		checkpoint.StopGitSnapshot = &stopSnapshot
	}
	if err := w.state.SaveResumeCheckpoint(checkpoint); err != nil {
		return checkpoint, outputPath, guardBefore, err
	}
	if err := w.acknowledgeSessionRotationStart(checkpoint); err != nil {
		return checkpoint, outputPath, guardBefore, err
	}
	if w.stopRequested() {
		return checkpoint, outputPath, guardBefore, w.interruptBetweenCalls(checkpoint)
	}
	w.state.RecordModelCall(checkpoint.Role, checkpoint.Model)
	return checkpoint, outputPath, guardBefore, nil
}

func (w *Workflow) acknowledgeSessionRotationStart(checkpoint state.ResumeCheckpoint) error {
	claimID := os.Getenv(state.SessionRotationClaimIDEnv)
	if checkpoint.Phase != "worker-new" || claimID == "" {
		return nil
	}
	return w.state.AcknowledgeSessionRotationClaim(claimID, os.Getenv(state.ParentActionCodexThreadIDEnv))
}

func (w *Workflow) applyModelArtifactContext(checkpoint *state.ResumeCheckpoint) error {
	switch checkpoint.Role {
	case state.WorkerRole:
		artifactDir, err := w.state.PrepareArtifactDir()
		if err != nil {
			return err
		}
		checkpoint.Prompt = withArtifactContext(checkpoint.Prompt, artifactDir)
		if checkpoint.OriginalPrompt != "" {
			checkpoint.OriginalPrompt = withArtifactContext(checkpoint.OriginalPrompt, artifactDir)
		}
	case state.ReviewerRole:
		taskID, err := w.state.TaskID()
		if err != nil {
			return err
		}
		artifactDir := w.state.ArtifactDir(taskID)
		checkpoint.Prompt = withReviewerArtifactContext(checkpoint.Prompt, artifactDir)
		if checkpoint.OriginalPrompt != "" {
			checkpoint.OriginalPrompt = withReviewerArtifactContext(checkpoint.OriginalPrompt, artifactDir)
		}
	}
	return nil
}

func (w *Workflow) invokeModelCall(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	guardBefore parentFileGuard,
) (modelCallExecution, error) {
	execution := modelCallExecution{startedAt: w.now().UTC()}
	w.modelCallAttempts++
	execution.runResult, execution.runErr = w.runner.Run(
		checkpoint.Role,
		checkpoint.Phase,
		checkpoint.Model,
		checkpoint.ReadOnly,
		checkpoint.Effort,
		checkpoint.Prompt,
		outputPath,
	)
	execution.completedAt = w.now().UTC()
	w.state.RecordModelDuration(checkpoint.Model, execution.completedAt.Sub(execution.startedAt))
	if stopped, err := w.verifyParentFileAfterCall(
		checkpoint,
		guardBefore,
		execution.runResult,
		execution.startedAt,
		execution.completedAt,
		execution.runErr,
		outputPath,
	); stopped {
		return execution, err
	}
	return execution, nil
}

func (w *Workflow) resolveModelCallFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) (modelCallExecution, error) {
	if execution.runErr == nil {
		return execution, nil
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(execution.runErr, &interrupted) {
		return execution, w.interruptFromCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, execution.runErr, outputPath)
	}
	if runner.IsRecoverableGuardFailure(execution.runErr) {
		return execution, w.saveGuardRecoverableState(checkpoint, execution, outputPath)
	}

	failureClass := mergePlainFailureClass(
		runner.ClassifyProviderFailureText(runner.ReadTransientSignal(outputPath)),
		execution.runResult.PlainFailure,
	)
	if failureClass.Kind == runner.ProviderFailureZaiFiveHour {
		return execution, w.saveRateLimitedState(checkpoint, failureClass.FiveHourLimit, execution.runResult, execution.startedAt, execution.completedAt, execution.runErr, outputPath)
	}
	if err := w.handleStructuredModelFailure(checkpoint, outputPath, execution); err != nil {
		return execution, err
	}
	if failureClass.Kind != runner.ProviderFailureTransient {
		return execution, nil
	}
	return w.resolveTransientModelFailure(checkpoint, outputPath, failureClass.Detail, execution)
}

func (w *Workflow) handleStructuredModelFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) error {
	var structuredErr *runner.StructuredOutputError
	if !errors.As(execution.runErr, &structuredErr) {
		return nil
	}
	if structuredErr.RetryExhausted() {
		w.state.RecordStructuredRetryExhausted()
	}
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "invalid_packet", "", execution.runErr, outputPath, callDiagnostics{})
	_ = w.state.ClearResumeCheckpoint()
	_ = w.state.RemoveUnreadySession(checkpoint.Role)
	return &WorkerError{
		Phase:   checkpoint.Phase + "-structured-output",
		Message: execution.runErr.Error(),
		Tail:    packet.Tail(outputPath, 20),
	}
}

func (w *Workflow) resolveTransientModelFailure(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	classification string,
	execution modelCallExecution,
) (modelCallExecution, error) {
	recovered, resumeResult, resumeStartedAt, resumeCompletedAt, recErr := w.recoverTransient(
		checkpoint,
		outputPath,
		classification,
		execution.runResult,
		execution.startedAt,
		execution.completedAt,
	)
	if recovered {
		execution.runResult = resumeResult
		execution.startedAt = resumeStartedAt
		execution.completedAt = resumeCompletedAt
		execution.runErr = nil
		return execution, nil
	}
	if errors.Is(recErr, errParentFileGuardStopped) {
		return execution, recErr
	}
	var interrupted *runner.InterruptedCallError
	if errors.As(recErr, &interrupted) {
		if resumeStartedAt.IsZero() {
			return execution, w.interruptBetweenCalls(checkpoint)
		}
		return execution, w.interruptFromCall(checkpoint, resumeResult, resumeStartedAt, resumeCompletedAt, recErr, outputPath)
	}
	var providerUnavailable *runner.ProviderUnavailableError
	if errors.As(recErr, &providerUnavailable) {
		_ = w.state.SecureArtifactDir()
		w.state.RecordProviderUnavailable(checkpoint.Model)
		return execution, recErr
	}
	var guardRecoverable *GuardRecoverableError
	if errors.As(recErr, &guardRecoverable) {
		return execution, recErr
	}
	var limitErr runner.ZaiRateLimitError
	if errors.As(recErr, &limitErr) {
		return execution, recErr
	}

	execution.runResult = resumeResult
	execution.startedAt = resumeStartedAt
	execution.completedAt = resumeCompletedAt
	execution.runErr = recErr
	execution.recoveryFatal = true
	return execution, nil
}

func (w *Workflow) finalizeModelCallState(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
) error {
	if err := w.state.SecureArtifactDir(); err != nil {
		if !execution.recoveryFatal {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		}
		return err
	}
	if execution.runErr != nil {
		if !execution.recoveryFatal {
			w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "error", "", execution.runErr, outputPath, callDiagnostics{})
		}
		_ = w.state.ClearResumeCheckpoint()
		_ = w.state.RemoveUnreadySession(checkpoint.Role)
		failure := workerError(checkpoint.Phase, outputPath, execution.runErr)
		if runner.IsPreCallGuardFailure(execution.runErr) {
			failure.cause = execution.runErr
		}
		return failure
	}
	if err := w.state.ClearResumeCheckpoint(); err != nil {
		w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}
	return nil
}

func (w *Workflow) parseModelCallResult(checkpoint state.ResumeCheckpoint, runResult runner.RunResult) (packet.Result, error) {
	result, err := packet.ParseStructured(runResult.StructuredOutput)
	if err != nil {
		return packet.Result{}, err
	}
	if checkpoint.Role == state.ReviewerRole {
		err = packet.ValidateReviewerResult(result)
		if err == nil && result.Status == packet.StatusNeedsSolDecision {
			err = w.validateReviewerDecisionBoundary()
		}
	} else {
		err = packet.ValidateWorkerResult(result)
	}
	return result, err
}

func (w *Workflow) handleInvalidModelResult(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	execution modelCallExecution,
	resultErr error,
) (packet.Result, error) {
	w.recordModelCall(checkpoint, execution.runResult, execution.startedAt, execution.completedAt, "invalid_packet", "", resultErr, outputPath, callDiagnostics{})
	if packet.IsConstraintError(resultErr) {
		return w.handleResultCorrectionViolation(checkpoint, resultErr)
	}
	if checkpoint.ResultCorrection {
		return packet.Result{}, w.resultCorrectionInvalidResponseFailure(checkpoint, resultErr)
	}
	return packet.Result{}, &WorkerError{
		Phase:   checkpoint.Phase + "-format",
		Message: resultErr.Error(),
		Tail:    packet.Tail(outputPath, 20),
	}
}

func (w *Workflow) saveRateLimitedState(
	checkpoint state.ResumeCheckpoint,
	limit runner.ZaiFiveHourLimit,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	runErr error,
	outputPath string,
) error {
	if err := w.state.MarkReady(checkpoint.Role); err != nil {
		w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}
	taskID, err := w.persistRateLimitedStop(checkpoint, limit)
	if err != nil {
		w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "state_error", "", err, outputPath, callDiagnostics{})
		return err
	}

	artifactErr := w.state.SecureArtifactDir()
	telemetryErr := runErr
	artifactWarning := ""
	if artifactErr != nil {
		artifactWarning = artifactErr.Error()
		telemetryErr = fmt.Errorf("%w; %w", runErr, artifactErr)
	}
	w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "rate_limited", "", telemetryErr, outputPath, callDiagnostics{})
	return runner.ZaiRateLimitError{
		Phase:           checkpoint.Phase,
		Limit:           limit,
		TaskID:          taskID,
		RepoRoot:        w.config.RepoRoot,
		RepoShort:       w.config.RepoShort,
		ArtifactWarning: artifactWarning,
	}
}

func (w *Workflow) persistRateLimitedStop(checkpoint state.ResumeCheckpoint, limit runner.ZaiFiveHourLimit) (string, error) {
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtCST = limit.ResetAtCST
	checkpoint.ResetAtRFC3339 = limit.ResetAtRFC3339

	_ = w.attachStopRepositoryBoundary(&checkpoint)
	if err := w.state.EnterStop(checkpoint); err != nil {
		return "", err
	}
	w.state.RecordRateLimit(checkpoint.Model)
	taskID, err := w.state.TaskID()
	if err != nil {
		return "", err
	}
	return taskID, nil
}

func (w *Workflow) saveProbeRateLimited(checkpoint state.ResumeCheckpoint, limit runner.ZaiFiveHourLimit) error {
	taskID, err := w.persistRateLimitedStop(checkpoint, limit)
	if err != nil {
		return err
	}
	_ = w.state.SecureArtifactDir()
	return runner.ZaiRateLimitError{
		Phase:     checkpoint.Phase,
		Limit:     limit,
		TaskID:    taskID,
		RepoRoot:  w.config.RepoRoot,
		RepoShort: w.config.RepoShort,
	}
}

func (w *Workflow) recoverTransient(
	checkpoint state.ResumeCheckpoint,
	outputPath string,
	classification string,
	initialResult runner.RunResult,
	initialStartedAt time.Time,
	initialCompletedAt time.Time,
) (bool, runner.RunResult, time.Time, time.Time, error) {
	w.recordModelCall(checkpoint, initialResult, initialStartedAt, initialCompletedAt, "transient_error", "", fmt.Errorf("transient provider failure: %s", classification), outputPath, callDiagnostics{providerClassification: classification})
	if err := w.state.MarkReady(checkpoint.Role); err != nil {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, err
	}
	return w.recoveryLoop(checkpoint, classification, false, func() (bool, runner.RunResult, time.Time, time.Time, error) {
		w.pendingRetry = &callRetryContext{callID: w.lastCallID, reason: transientRetryReason(classification)}
		return w.runResumedTask(checkpoint, outputPath)
	})
}

func transientRetryReason(classification string) string {
	return "transient-provider-failure:" + classification
}

func (w *Workflow) gateResumeOnProbe(checkpoint state.ResumeCheckpoint) error {
	_, _, _, _, err := w.recoveryLoop(checkpoint, checkpoint.ProviderUnavailableClassification, true, func() (bool, runner.RunResult, time.Time, time.Time, error) {
		return true, runner.RunResult{}, time.Time{}, time.Time{}, nil
	})
	return err
}

func (w *Workflow) runResumedTask(checkpoint state.ResumeCheckpoint, outputPath string) (bool, runner.RunResult, time.Time, time.Time, error) {

	guardBefore, stopped, err := w.captureParentFileGuard(checkpoint.Role)
	if stopped {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, err
	}
	startedAt := w.now().UTC()

	w.state.RecordTransientRetry()
	w.state.RecordModelCall(checkpoint.Role, checkpoint.Model)
	w.modelCallAttempts++
	result, runErr := w.runner.Run(
		checkpoint.Role,
		checkpoint.Phase,
		checkpoint.Model,
		checkpoint.ReadOnly,
		checkpoint.Effort,
		checkpoint.Prompt,
		outputPath,
	)
	completedAt := w.now().UTC()
	w.state.RecordModelDuration(checkpoint.Model, completedAt.Sub(startedAt))
	if stopped, err := w.verifyParentFileAfterCall(checkpoint, guardBefore, result, startedAt, completedAt, runErr, outputPath); stopped {
		return false, result, startedAt, completedAt, err
	}

	var interrupted *runner.InterruptedCallError
	if runErr != nil && errors.As(runErr, &interrupted) {
		return false, result, startedAt, completedAt, runErr
	}
	if runErr != nil && runner.IsRecoverableGuardFailure(runErr) {
		execution := modelCallExecution{
			runResult:   result,
			startedAt:   startedAt,
			completedAt: completedAt,
			runErr:      runErr,
		}
		return false, result, startedAt, completedAt, w.saveGuardRecoverableState(checkpoint, execution, outputPath)
	}
	if runErr == nil {
		return true, result, startedAt, completedAt, nil
	}
	class := mergePlainFailureClass(
		runner.ClassifyProviderFailureText(runner.ReadTransientSignal(outputPath)),
		result.PlainFailure,
	)
	if class.Kind == runner.ProviderFailureZaiFiveHour {
		err := w.saveRateLimitedState(checkpoint, class.FiveHourLimit, result, startedAt, completedAt, runErr, outputPath)
		return false, result, startedAt, completedAt, err
	}
	if class.Kind != runner.ProviderFailureTransient {

		w.recordModelCall(checkpoint, result, startedAt, completedAt, "error", "", runErr, outputPath, callDiagnostics{})
		return false, result, startedAt, completedAt, runErr
	}
	w.recordModelCall(checkpoint, result, startedAt, completedAt, "transient_error", "", runErr, outputPath, callDiagnostics{providerClassification: class.Detail})
	return false, runner.RunResult{}, startedAt, completedAt, nil
}

func mergePlainFailureClass(base runner.ProviderFailureClass, plain runner.ProviderFailureClass) runner.ProviderFailureClass {
	switch {
	case plain.Kind == runner.ProviderFailureZaiFiveHour:
		return plain
	case base.Kind == runner.ProviderFailureZaiFiveHour:
		return base
	case base.Kind == runner.ProviderFailureTransient:
		return base
	case plain.Kind == runner.ProviderFailureTransient:
		return plain
	default:
		return base
	}
}

func (w *Workflow) recoveryLoop(
	checkpoint state.ResumeCheckpoint,
	classification string,
	firstProbeImmediate bool,
	onProbeSuccess func() (bool, runner.RunResult, time.Time, time.Time, error),
) (bool, runner.RunResult, time.Time, time.Time, error) {
	recoveryStart := w.now().UTC()
	deadline := recoveryStart.Add(providerUnavailableDeadline)
	probes := 0
	sleeps := 0
	exhaustClassification := classification

	for probes < maxTransientProbes {
		nextSleeps, proceed, err := w.waitForRecoveryProbe(checkpoint, firstProbeImmediate, probes, sleeps, deadline)
		if err != nil {
			return false, runner.RunResult{}, time.Time{}, time.Time{}, err
		}
		if !proceed {
			break
		}
		sleeps = nextSleeps

		probes++
		done, recovered, result, startedAt, completedAt, nextClassification, err := w.runRecoveryAttempt(checkpoint, probes, onProbeSuccess)
		if nextClassification != "" {
			exhaustClassification = nextClassification
		}
		if err != nil {
			return false, result, startedAt, completedAt, err
		}
		if done {
			return recovered, result, startedAt, completedAt, nil
		}
	}

	pErr, saveErr := w.saveProviderUnavailable(checkpoint, exhaustClassification, probes, recoveryStart)
	if saveErr != nil {
		return false, runner.RunResult{}, time.Time{}, time.Time{}, saveErr
	}
	return false, runner.RunResult{}, time.Time{}, time.Time{}, pErr
}

func (w *Workflow) waitForRecoveryProbe(
	checkpoint state.ResumeCheckpoint,
	firstProbeImmediate bool,
	probes int,
	sleeps int,
	deadline time.Time,
) (int, bool, error) {
	if firstProbeImmediate && probes == 0 {
		return sleeps, true, nil
	}
	wait, ok := w.backoffWait(sleeps, deadline)
	if !ok {
		return sleeps, false, nil
	}
	if w.sleepInterruptible(wait) {
		return sleeps, false, &runner.InterruptedCallError{Phase: checkpoint.Phase}
	}
	sleeps++
	return sleeps, !w.now().After(deadline), nil
}

func (w *Workflow) runRecoveryAttempt(
	checkpoint state.ResumeCheckpoint,
	attempt int,
	onProbeSuccess func() (bool, runner.RunResult, time.Time, time.Time, error),
) (bool, bool, runner.RunResult, time.Time, time.Time, string, error) {
	success, classification, startedAt, completedAt, err := w.runRecoveryProbe(checkpoint, attempt)
	if err != nil || !success {
		return false, false, runner.RunResult{}, startedAt, completedAt, classification, err
	}
	recovered, result, startedAt, completedAt, err := onProbeSuccess()
	return recovered || err != nil, recovered, result, startedAt, completedAt, classification, err
}

func (w *Workflow) runRecoveryProbe(checkpoint state.ResumeCheckpoint, attempt int) (bool, string, time.Time, time.Time, error) {
	startedAt := w.now().UTC()
	probeResult, probeErr := w.runner.Probe(checkpoint.Model)
	completedAt := w.now().UTC()
	if probeErr == nil {
		if contractErr := runner.ValidateProbeResult(probeResult); contractErr != nil {
			probeErr = &runner.ProbeInvalidResponseError{Model: checkpoint.Model, Reason: contractErr}
		}
	}
	w.recordProbeCall(checkpoint, probeResult, attempt, startedAt, completedAt, probeErr)
	if probeErr == nil {
		return true, "", startedAt, completedAt, nil
	}

	class := runner.ClassifyProviderFailureText(probeErr.Error())
	if class.Kind == runner.ProviderFailureZaiFiveHour {
		return false, "", startedAt, completedAt, w.saveProbeRateLimited(checkpoint, class.FiveHourLimit)
	}
	if class.Kind == runner.ProviderFailureTransient {
		return false, "", startedAt, completedAt, nil
	}
	var probeInvalid *runner.ProbeInvalidResponseError
	if errors.As(probeErr, &probeInvalid) && !runner.DetectProbeFatalSignal(probeErr.Error()) {
		return false, runner.ProbeContractFailure, startedAt, completedAt, nil
	}
	return false, "", startedAt, completedAt, probeErr
}

func (w *Workflow) backoffWait(sleeps int, deadline time.Time) (time.Duration, bool) {
	if sleeps >= len(transientBackoffSchedule) {
		return 0, false
	}
	remaining := deadline.Sub(w.now())
	if remaining <= 0 {
		return 0, false
	}
	wait := w.jitter(transientBackoffSchedule[sleeps])
	if wait > remaining {
		wait = remaining
	}
	return wait, true
}

func (w *Workflow) saveProviderUnavailable(checkpoint state.ResumeCheckpoint, classification string, probes int, recoveryStart time.Time) (*runner.ProviderUnavailableError, error) {
	checkpoint.SetStopKind(state.ResumeStopProviderUnavailable)
	checkpoint.ProviderUnavailableClassification = classification
	checkpoint.ProviderUnavailableProbes = probes
	checkpoint.ProviderUnavailableStartedAt = recoveryStart

	_ = w.attachStopRepositoryBoundary(&checkpoint)
	if err := w.state.EnterStop(checkpoint); err != nil {
		return nil, err
	}
	elapsed := w.now().Sub(recoveryStart)
	w.recordProviderUnavailableEvent(checkpoint, classification, probes, elapsed)
	taskID, _ := w.state.TaskID()
	return &runner.ProviderUnavailableError{
		Phase:          checkpoint.Phase,
		Classification: classification,
		Probes:         probes,
		Elapsed:        elapsed,
		TaskID:         taskID,
		RepoRoot:       w.config.RepoRoot,
		RepoShort:      w.config.RepoShort,
	}, nil
}

func (w *Workflow) interruptFromCall(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	runErr error,
	outputPath string,
) error {
	w.recordModelCall(checkpoint, runResult, startedAt, completedAt, "interrupted", "", runErr, outputPath, callDiagnostics{})
	return w.persistInterruptedStop(checkpoint, runErr)
}

func (w *Workflow) interruptBetweenCalls(checkpoint state.ResumeCheckpoint) error {
	w.recordInterruptedEvent(checkpoint)
	return w.persistInterruptedStop(checkpoint, nil)
}

func (w *Workflow) persistInterruptedStop(checkpoint state.ResumeCheckpoint, cause error) error {
	checkpoint.SetStopKind(state.ResumeStopInterrupted)

	_ = w.attachStopRepositoryBoundary(&checkpoint)

	if files, filesErr := state.CaptureStopDirtyFiles(w.config.RepoRoot); filesErr == nil {
		checkpoint.StopDirtyFiles = files
	}

	_ = state.CaptureStopPatches(w.config, w.state)

	if w.state.ReadOr(string(checkpoint.Role)+".id", "") != "" {
		if err := w.state.MarkReady(checkpoint.Role); err != nil {
			return err
		}
	}
	if err := w.state.EnterStop(checkpoint); err != nil {
		return err
	}
	_ = w.state.SecureArtifactDir()
	taskID, _ := w.state.TaskID()

	cleanupWarning := ""
	if cause != nil {
		var interrupted *runner.InterruptedCallError
		if errors.As(cause, &interrupted) {
			cleanupWarning = interrupted.CleanupWarning
		}
	}
	if w.stop != nil {
		w.stop.NotifyInterrupted(taskID, cleanupWarning)
	}
	stopped := &runner.InterruptedCallError{
		Phase:          checkpoint.Phase,
		TaskID:         taskID,
		RepoRoot:       w.config.RepoRoot,
		CleanupWarning: cleanupWarning,
	}
	return stopped
}

func (w *Workflow) recordInterruptedEvent(checkpoint state.ResumeCheckpoint) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       checkpoint.Phase + "-user-interrupted",
		Role:        checkpoint.Role,
		ModelAlias:  checkpoint.Model,
		Outcome:     "user_interrupted",
	})
}

func (w *Workflow) sleepInterruptible(duration time.Duration) bool {
	if w.stop == nil {
		w.sleep(duration)
		return false
	}
	done := make(chan struct{})
	go func() {
		w.sleep(duration)
		close(done)
	}()
	select {
	case <-done:
		return false
	case <-w.stop.Requested():
		return true
	}
}

func (w *Workflow) recordProviderUnavailableEvent(checkpoint state.ResumeCheckpoint, classification string, probes int, elapsed time.Duration) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:                 w.state.ReadOr("task.id", "unknown"),
		CallType:               state.CallTypeEvent,
		StartedAt:              now,
		CompletedAt:            now,
		Phase:                  checkpoint.Phase + "-provider-unavailable",
		Role:                   checkpoint.Role,
		ModelAlias:             checkpoint.Model,
		Outcome:                "provider_unavailable",
		ProviderClassification: classification,
		ProbeAttempt:           probes,
		RetryElapsedMS:         elapsed.Milliseconds(),
	})
}

func (w *Workflow) recordProbeCall(
	checkpoint state.ResumeCheckpoint,
	probe runner.ProbeResult,
	attempt int,
	startedAt time.Time,
	completedAt time.Time,
	probeErr error,
) {
	outcome := "probe_success"
	errorText := ""
	if probeErr != nil {
		outcome = "probe_failure"
		errorText = boundedText(probeErr.Error(), packet.MaxDiagnosticBytes)
	}
	w.state.RecordProbeOutcome(outcome)
	promptHash := sha256.Sum256([]byte(runner.ProbePrompt))
	response := probe.Response
	if !w.config.TelemetryContent {
		response = ""
	}
	resolvedUsage := make(map[string]state.ResolvedModelUsage, len(probe.ModelUsage))
	for model, usage := range probe.ModelUsage {
		resolvedUsage[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:              w.state.ReadOr("task.id", "unknown"),
		CallType:            state.CallTypeProbe,
		SessionID:           "none",
		StartedAt:           startedAt,
		CompletedAt:         completedAt,
		Phase:               fmt.Sprintf("%s-probe-%d", checkpoint.Phase, attempt),
		Role:                checkpoint.Role,
		ModelAlias:          checkpoint.Model,
		ResolvedModelUsage:  resolvedUsage,
		Effort:              "low",
		ReadOnly:            true,
		Outcome:             outcome,
		ProbeAttempt:        attempt,
		PromptBytes:         len(runner.ProbePrompt),
		PromptSHA256:        hex.EncodeToString(promptHash[:]),
		Response:            response,
		ResponseBytes:       len(probe.Response),
		Error:               errorText,
		TopLevelUsage:       state.TokenUsage(probe.Usage),
		WallDurationMS:      completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:    probe.DurationMS,
		ClaudeAPIDurationMS: probe.DurationAPIMS,
		TotalCostUSD:        probe.TotalCostUSD,
	})
}

func (w *Workflow) recordModelCall(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
	diag callDiagnostics,
) {
	entry := w.buildModelCallLog(checkpoint, runResult, startedAt, completedAt, outcome, packetStatus, callErr, outputPath)
	if entry.CallID == "" {
		if callID, err := state.NewUUID(); err == nil {
			entry.CallID = callID
		}
	}
	w.applyCallDiagnostics(&entry, checkpoint, outcome, callErr, diag)
	w.state.RecordModelCallLog(entry)
	w.lastCallID = entry.CallID
}

func (w *Workflow) buildModelCallLog(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
) state.ModelCallLog {
	response := runResult.Response
	if response == "" {
		response = packet.Tail(outputPath, packet.MaxDiagnosticBytes)
	}
	promptHash := sha256.Sum256([]byte(checkpoint.Prompt))
	responseHash := sha256.Sum256([]byte(response))
	errorText := modelCallErrorText(callErr)
	promptContent, systemPromptContent, responseContent := w.telemetryContents(checkpoint.Prompt, runResult.SystemPrompt, response)
	return state.ModelCallLog{
		CallID:                            runResult.CallID,
		TaskID:                            w.state.ReadOr("task.id", "unknown"),
		CallType:                          state.CallTypeTask,
		SessionID:                         modelSessionID(w.state, checkpoint.Role, runResult.SessionID),
		StartedAt:                         startedAt,
		CompletedAt:                       completedAt,
		Phase:                             checkpoint.Phase,
		Role:                              checkpoint.Role,
		ModelAlias:                        checkpoint.Model,
		ResolvedModelID:                   runResult.ResolvedModelID,
		ConfiguredAutoCompactWindowTokens: runResult.ConfiguredAutoCompactWindowTokens,
		KnownModelContextWindowTokens:     runResult.KnownModelContextWindowTokens,
		DeclaredMaxContextWindowTokens:    runResult.DeclaredMaxContextWindowTokens,
		ContextWindowSource:               runResult.ContextWindowSource,
		ResolvedModelUsage:                resolvedModelUsage(runResult.ModelUsage),
		Effort:                            checkpoint.Effort,
		ReadOnly:                          checkpoint.ReadOnly,
		Resumed:                           runResult.Resumed,
		Outcome:                           outcome,
		PacketStatus:                      packetStatus,
		Prompt:                            promptContent,
		PromptBytes:                       len([]byte(checkpoint.Prompt)),
		PromptSHA256:                      hex.EncodeToString(promptHash[:]),
		SystemPromptBytes:                 runResult.SystemPromptBytes,
		SystemPromptSHA256:                runResult.SystemPromptSHA256,
		SystemPrompt:                      systemPromptContent,
		Response:                          responseContent,
		ResponseBytes:                     len([]byte(response)),
		ResponseSHA256:                    hex.EncodeToString(responseHash[:]),
		Error:                             errorText,
		TopLevelUsage:                     topLevelUsage(runResult.TopLevelUsage),
		Runtime:                           runResult.Runtime,
		WallDurationMS:                    completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:                  runResult.DurationMS,
		ClaudeAPIDurationMS:               runResult.DurationAPIMS,
		TopLevelTurns:                     runResult.TopLevelTurns,
		TotalCostUSD:                      runResult.TotalCostUSD,
	}
}

func topLevelUsage(usage runner.TokenUsage) state.TokenUsage {
	return state.TokenUsage{
		InputTokens:              usage.InputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		OutputTokens:             usage.OutputTokens,
	}
}

func modelCallErrorText(callErr error) string {
	if callErr == nil {
		return ""
	}
	return boundedText(callErr.Error(), packet.MaxDiagnosticBytes)
}

func resolvedModelUsage(usageByModel map[string]runner.ModelUsage) map[string]state.ResolvedModelUsage {
	resolved := make(map[string]state.ResolvedModelUsage, len(usageByModel))
	for model, usage := range usageByModel {
		resolved[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	return resolved
}

func (w *Workflow) telemetryContents(prompt string, systemPrompt string, response string) (string, string, string) {
	if !w.config.TelemetryContent {
		return "", "", ""
	}
	return prompt, systemPrompt, response
}

func (w *Workflow) applyCallDiagnostics(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint, outcome string, callErr error, diag callDiagnostics) {
	if diag.reportedRisk != "" {
		if checkpoint.Role == state.ReviewerRole {
			entry.ReviewerReportedRisk = diag.reportedRisk
		} else {
			entry.WorkerReportedRisk = diag.reportedRisk
		}
	}
	w.applyEffectiveRiskDiagnostic(entry, checkpoint)
	if diag.providerClassification != "" {
		entry.ProviderClassification = diag.providerClassification
	}
	if w.currentResumeSource != "" {
		entry.ResumeSource = w.currentResumeSource
		w.currentResumeSource = ""
	}
	if w.pendingRetry != nil {
		entry.RetryOf = w.pendingRetry.callID
		entry.RetryReason = w.pendingRetry.reason
		w.pendingRetry = nil
	}
	if outcome == "invalid_packet" && callErr != nil {
		category := packet.RejectCategory(callErr)
		if runner.IsStructuredOutputError(callErr) {
			category = "structured-output"
		}
		entry.PacketRejectReason = category
		w.state.RecordPacketReject(category)
	}
	if checkpoint.Role == state.ReviewerRole && outcome == "success" && w.pendingSnapshot != nil {
		entry.Snapshot = w.pendingSnapshot
		w.pendingSnapshot = nil
	}
}

func (w *Workflow) applyEffectiveRiskDiagnostic(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint) {
	if checkpoint.EffectiveRisk == "" {
		return
	}
	entry.EffectiveRisk = checkpoint.EffectiveRisk
	entry.RiskFloorSource = checkpoint.EffectiveRiskSource
	if checkpoint.Role != state.ReviewerRole || checkpoint.EffectiveRisk != highRiskValue {
		return
	}
	category := riskFloorCategory(checkpoint.EffectiveRiskSource)
	entry.RiskFloorCategory = category
	w.state.RecordRiskFloor(category)
}

func riskFloorCategory(source string) string {
	if source == "" {
		return ""
	}
	var categories []string
	for _, raw := range strings.Split(source, ";") {
		name := strings.SplitN(raw, ":", 2)[0]
		if name != "" {
			categories = append(categories, name)
		}
	}
	return strings.Join(categories, ",")
}

func modelSessionID(st *state.StateStore, role state.SessionRole, fromRunner string) string {
	if fromRunner != "" {
		return fromRunner
	}
	return st.ReadOr(string(role)+".id", "unknown")
}
