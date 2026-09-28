package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathtrial"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type failurePathTrialRunner interface {
	RunWithDeadline(role state.SessionRole, phase string, model string, readOnly bool, effort string, prompt string, outputPath string, deadline time.Time) (runner.RunResult, error)
}

const failurePathTrialCallDeadline = 10 * time.Minute

const failurePathTrialDetailBoundBytes = 2048

const failurePathMeasurementFailedOutcome = "trial_measurement_failed"

func (w *Workflow) observeFailurePathTrial(request string, reviewResult packet.Result, reviewNumber int) {
	if !w.config.FailurePathTrial {
		return
	}
	taskID, err := w.state.TaskID()
	if err != nil || taskID == "" {
		return
	}
	registryPath := w.state.Path(failurepathtrial.RegistryFile)
	registry, err := failurepathtrial.LoadRegistry(registryPath)
	if err != nil {
		w.recordFailurePathMeasurementFailure("registry-load", err)
		return
	}
	if registry.HasTaskRecord(taskID) {
		return
	}
	if w.stopRequested() {
		return
	}
	trigger, triggerErr := w.classifyFailurePathTrigger()
	if triggerErr != nil {
		w.appendFailurePathTrialRecord(registryPath, w.newFailurePathTrialRecord(
			taskID, reviewNumber, "", trigger, failurepathtrial.OutcomeClassificationMissing, nil, "", triggerErr.Error(),
		))
		return
	}
	if !trigger.Triggered {
		if len(trigger.AmbiguousClasses) == 0 {
			return
		}
		w.appendFailurePathTrialRecord(registryPath, w.newFailurePathTrialRecord(
			taskID, reviewNumber, "", trigger, failurepathtrial.OutcomeAmbiguous, nil, "", "",
		))
		return
	}
	if registry.CohortSize() >= failurepathtrial.CohortCap {
		w.appendFailurePathTrialRecord(registryPath, w.newFailurePathTrialRecord(
			taskID, reviewNumber, "", trigger, failurepathtrial.OutcomeCapped, nil, "", "",
		))
		return
	}
	record := w.runFailurePathTrialReviewer(taskID, request, reviewResult, reviewNumber, trigger)
	w.appendFailurePathTrialRecord(registryPath, record)
}

func (w *Workflow) appendFailurePathTrialRecord(path string, record failurepathtrial.Record) {
	if _, err := failurepathtrial.AppendRecord(path, record); err != nil {
		stage := "registry-save"
		if errors.Is(err, failurepathtrial.ErrCohortFull) {
			stage = "cohort-cap"
		}
		w.recordFailurePathMeasurementFailure(stage, fmt.Errorf("outcome=%s call_id=%s: %w", record.Outcome, record.CallID, err))
	}
}

func (w *Workflow) recordFailurePathMeasurementFailure(stage string, cause error) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       "failure-path-reviewer-measurement",
		Role:        state.FailurePathReviewerRole,
		Outcome:     failurePathMeasurementFailedOutcome,
		Error:       boundedText(stage+": "+cause.Error(), packet.MaxDiagnosticBytes),
	})
}

func (w *Workflow) classifyFailurePathTrigger() (failurepathtrial.TriggerDecision, error) {
	baselineHead := w.state.ReadOr("baseline-head", "")
	paths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
	if err != nil {
		return failurepathtrial.TriggerDecision{}, err
	}
	return failurepathtrial.ClassifyTrigger(w.config.RepoRoot, baselineHead, paths)
}

func (w *Workflow) runFailurePathTrialReviewer(
	taskID string,
	request string,
	reviewResult packet.Result,
	reviewNumber int,
	trigger failurepathtrial.TriggerDecision,
) failurepathtrial.Record {
	phase := fmt.Sprintf("failure-path-reviewer-%d", reviewNumber)
	model := w.config.HighRiskReviewerModel
	trialRunner, available := w.runner.(failurePathTrialRunner)
	if !available {
		return w.newFailurePathTrialRecord(
			taskID, reviewNumber, phase, trigger, failurepathtrial.OutcomeMissingRunUnavailable, nil, "",
			"runnerがdeadline付きshadow呼出に対応していません",
		)
	}
	prompt := failurePathTrialPrompt(request, reviewNumber, trigger)
	outputPath := filepath.Join(w.temp, phase+".log")
	startedAt := w.now().UTC()
	result, runErr := trialRunner.RunWithDeadline(
		state.FailurePathReviewerRole, phase, model, true, w.config.RoutineEffort, prompt, outputPath,
		startedAt.Add(failurePathTrialCallDeadline),
	)
	completedAt := w.now().UTC()
	outcome, detail := resolveFailurePathTrialOutcome(runErr)
	findings := []failurepathtrial.Finding{}
	if outcome == failurepathtrial.OutcomeObserved {
		parsed, parseErr := failurepathtrial.ParseShadowOutput(result.StructuredOutput)
		if parseErr != nil {
			outcome = failurepathtrial.OutcomeMissingSchemaInvalid
			detail = parseErr.Error()
		} else {
			findings = parsed
		}
	}
	callID := w.recordFailurePathTrialCall(phase, prompt, model, result, startedAt, completedAt, outcome, runErr, outputPath)
	record := w.newFailurePathTrialRecord(taskID, reviewNumber, phase, trigger, outcome, findings, callID, detail)
	record.ReviewPacketStatus = string(reviewResult.Status)
	record.ReviewIssues = boundedText(reviewResult.Issues, failurePathTrialDetailBoundBytes)
	record.ReviewTargets = reviewResult.Targets
	record.AddedGLM = failurePathTrialUsage(result, startedAt, completedAt)
	return record
}

func resolveFailurePathTrialOutcome(runErr error) (string, string) {
	if runErr == nil {
		return failurepathtrial.OutcomeObserved, ""
	}
	var interrupted *runner.InterruptedCallError
	switch {
	case errors.As(runErr, &interrupted):
		return failurepathtrial.OutcomeMissingInterrupted, runErr.Error()
	case errors.Is(runErr, runner.ErrProbeDeadlineExceeded):
		return failurepathtrial.OutcomeMissingDeadline, runErr.Error()
	case runner.IsStructuredOutputError(runErr):
		return failurepathtrial.OutcomeMissingPartial, runErr.Error()
	default:
		return failurepathtrial.OutcomeMissingProvider, runErr.Error()
	}
}

func (w *Workflow) recordFailurePathTrialCall(
	phase, prompt, model string,
	result runner.RunResult,
	startedAt, completedAt time.Time,
	outcome string,
	runErr error,
	outputPath string,
) string {
	checkpoint := state.ResumeCheckpoint{
		Phase:    phase,
		Role:     state.FailurePathReviewerRole,
		Model:    model,
		ReadOnly: true,
		Effort:   w.config.RoutineEffort,
		Prompt:   prompt,
	}
	entry := w.buildModelCallLog(checkpoint, result, startedAt, completedAt, outcome, "", runErr, outputPath)
	if entry.CallID == "" {
		if callID, err := state.NewUUID(); err == nil {
			entry.CallID = callID
		}
	}
	w.state.RecordModelCallLog(entry)
	w.state.RecordModelCall(state.FailurePathReviewerRole, model)
	w.state.RecordModelDuration(model, completedAt.Sub(startedAt))
	return entry.CallID
}

func failurePathTrialUsage(result runner.RunResult, startedAt, completedAt time.Time) *failurepathtrial.AddedGLMUsage {
	return &failurepathtrial.AddedGLMUsage{
		Calls:                    1,
		InputTokens:              result.TopLevelUsage.InputTokens,
		CacheCreationInputTokens: result.TopLevelUsage.CacheCreationInputTokens,
		CacheReadInputTokens:     result.TopLevelUsage.CacheReadInputTokens,
		OutputTokens:             result.TopLevelUsage.OutputTokens,
		TotalCostUSD:             result.TotalCostUSD,
		WallDurationMS:           completedAt.Sub(startedAt).Milliseconds(),
		ClaudeAPIDurationMS:      result.DurationAPIMS,
	}
}

func (w *Workflow) newFailurePathTrialRecord(
	taskID string,
	reviewNumber int,
	phase string,
	trigger failurepathtrial.TriggerDecision,
	outcome string,
	findings []failurepathtrial.Finding,
	callID string,
	detail string,
) failurepathtrial.Record {
	record := failurepathtrial.Record{
		TaskID:           taskID,
		ReviewNumber:     reviewNumber,
		Phase:            phase,
		Outcome:          outcome,
		Classes:          trigger.Classes,
		AmbiguousClasses: trigger.AmbiguousClasses,
		TriggerPaths:     trigger.TriggerPaths,
		CallID:           callID,
		Findings:         findings,
		Detail:           boundedText(detail, failurePathTrialDetailBoundBytes),
		RecordedAt:       w.now().UTC().Format(time.RFC3339),
	}
	return record
}

func failurePathTrialPrompt(request string, reviewNumber int, trigger failurepathtrial.TriggerDecision) string {
	return fmt.Sprintf(`FAILURE_PATH_TRIAL_MODE: SHADOW_OBSERVATION

USER_REQUEST:
%s

TRIGGER_CLASSES:
%s

TRIGGER_PATHS:
%s

REVIEW_NUMBER: %d

上記classとpathに限定して今回の変更の失敗経路を検証してください。この結果は観測recordへだけ記録され、通常reviewやacceptの判定には使いません。
`, strings.TrimSpace(request), strings.Join(trigger.Classes, "\n"), strings.Join(trigger.TriggerPaths, "\n"), reviewNumber)
}
