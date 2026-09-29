package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type failurePathAdvisoryRunner interface {
	RunWithDeadline(role state.SessionRole, phase string, model string, readOnly bool, effort string, prompt string, outputPath string, deadline time.Time) (runner.RunResult, error)
}

const failurePathAdvisoryCallDeadline = 10 * time.Minute

const failurePathAdvisoryDetailBoundBytes = 2048

const failurePathAdvisoryReviewIssuesBoundBytes = 768

const failurePathAdvisoryRequestBoundBytes = 1536

const failurePathAdvisoryClassesBoundBytes = 512

const failurePathAdvisoryTriggerPathsBoundBytes = 768

const failurePathAdvisoryDiffReadBytes = 64 * 1024

const failurePathAdvisoryDiffBoundBytes = 3072

const failurePathAdvisoryPromptMaxBytes = 8192

const failurePathMeasurementFailedOutcome = "advisory_measurement_failed"

func (w *Workflow) attachFailurePathAdvisory(request string, reviewResult packet.Result, reviewNumber int) packet.Result {
	if !w.config.FailurePathAdvisory || !packet.AdvisoryVisibleStatus(reviewResult.Status) {
		return reviewResult
	}
	taskID, err := w.state.TaskID()
	if err != nil || taskID == "" {
		return reviewResult
	}
	registryPath := w.state.Path(failurepathadvisory.RegistryFile)
	registry, err := failurepathadvisory.LoadRegistry(registryPath)
	if err != nil {
		w.recordFailurePathMeasurementFailure("registry-load", err)
		return reviewResult
	}
	if registry.HasTaskRecord(taskID) || w.stopRequested() {
		return reviewResult
	}
	trigger, proceed := w.failurePathAdvisoryTriggerGate(taskID, reviewNumber, registry, registryPath)
	if !proceed {
		return reviewResult
	}
	diffText, available := w.failurePathAdvisoryDiffInput(taskID, reviewNumber, registryPath, trigger)
	if !available {
		return reviewResult
	}
	record := w.runFailurePathAdvisoryReviewer(taskID, request, reviewResult, reviewNumber, trigger, diffText)
	record, attached := resolveFailurePathAdvisoryAttachment(reviewResult, record)
	if err := w.appendFailurePathAdvisoryRecord(registryPath, record); err != nil {
		return reviewResult
	}
	reviewResult.FailurePathAdvisory = attached
	return reviewResult
}

func (w *Workflow) failurePathAdvisoryTriggerGate(
	taskID string,
	reviewNumber int,
	registry failurepathadvisory.Registry,
	registryPath string,
) (failurepathadvisory.TriggerDecision, bool) {
	trigger, triggerErr := w.classifyFailurePathTrigger()
	if triggerErr != nil {
		_ = w.appendFailurePathAdvisoryRecord(registryPath, w.newFailurePathAdvisoryRecord(
			taskID, reviewNumber, "", trigger, failurepathadvisory.OutcomeClassificationMissing, nil, "", triggerErr.Error(),
		))
		return trigger, false
	}
	if !trigger.Triggered {
		if len(trigger.AmbiguousClasses) == 0 {
			return trigger, false
		}
		_ = w.appendFailurePathAdvisoryRecord(registryPath, w.newFailurePathAdvisoryRecord(
			taskID, reviewNumber, "", trigger, failurepathadvisory.OutcomeAmbiguous, nil, "", "",
		))
		return trigger, false
	}
	if registry.CohortSize() >= failurepathadvisory.CohortCap {
		_ = w.appendFailurePathAdvisoryRecord(registryPath, w.newFailurePathAdvisoryRecord(
			taskID, reviewNumber, "", trigger, failurepathadvisory.OutcomeCapped, nil, "", "",
		))
		return trigger, false
	}
	return trigger, true
}

func fetchFailurePathAdvisoryDiff(repoRoot, baselineHead string, paths []string) (string, error) {
	return failurepathadvisory.DiffTextForPaths(repoRoot, baselineHead, paths, failurePathAdvisoryDiffReadBytes)
}

func (w *Workflow) failurePathAdvisoryDiffInput(
	taskID string,
	reviewNumber int,
	registryPath string,
	trigger failurepathadvisory.TriggerDecision,
) (string, bool) {
	diffText, err := w.fetchAdvisoryDiff(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""), trigger.TriggerPaths)
	if err == nil {
		return diffText, true
	}
	_ = w.appendFailurePathAdvisoryRecord(registryPath, w.newFailurePathAdvisoryRecord(
		taskID, reviewNumber, "", trigger, failurepathadvisory.OutcomeMissingDiff, nil, "", err.Error(),
	))
	return "", false
}

func resolveFailurePathAdvisoryAttachment(reviewResult packet.Result, record failurepathadvisory.Record) (failurepathadvisory.Record, *packet.FailurePathAdvisory) {
	verified := 0
	indeterminate := 0
	for _, finding := range record.Findings {
		if finding.Status == failurepathadvisory.FindingStatusIndeterminate {
			indeterminate++
			continue
		}
		verified++
	}
	if record.Outcome != failurepathadvisory.OutcomeObserved {
		record.Advisory = &failurepathadvisory.AdvisoryOutcome{
			Status:        failurepathadvisory.AdvisoryOmittedFailOpen,
			Indeterminate: indeterminate,
		}
		return record, nil
	}
	if verified == 0 {
		record.Advisory = &failurepathadvisory.AdvisoryOutcome{
			Status:        failurepathadvisory.AdvisoryOmittedNoFind,
			Indeterminate: indeterminate,
		}
		return record, nil
	}
	bounded := packet.BoundFailurePathAdvisory(reviewResult, packetAdvisoryFromFindings(record.CallID, record.Findings))
	if bounded == nil {
		record.Advisory = &failurepathadvisory.AdvisoryOutcome{
			Status:        failurepathadvisory.AdvisoryOmittedBound,
			Indeterminate: indeterminate,
			Truncated:     true,
		}
		return record, nil
	}
	record.Advisory = &failurepathadvisory.AdvisoryOutcome{
		Status:        failurepathadvisory.AdvisoryShown,
		FindingsShown: len(bounded.Findings),
		Indeterminate: len(bounded.Indeterminate),
		Truncated:     bounded.Truncated,
	}
	return record, bounded
}

func packetAdvisoryFromFindings(callID string, findings []failurepathadvisory.Finding) *packet.FailurePathAdvisory {
	advisory := &packet.FailurePathAdvisory{CallID: callID}
	for _, finding := range findings {
		if finding.Status == failurepathadvisory.FindingStatusIndeterminate {
			advisory.Indeterminate = append(advisory.Indeterminate, packet.FailurePathAdvisoryIndeterminate{
				Target: finding.Target,
				Class:  finding.Class,
			})
			continue
		}
		advisory.Findings = append(advisory.Findings, packet.FailurePathAdvisoryFinding{
			Target: finding.Target,
			Class:  finding.Class,
			Issue:  finding.Issue,
		})
	}
	return advisory
}

func (w *Workflow) appendFailurePathAdvisoryRecord(path string, record failurepathadvisory.Record) error {
	if _, err := failurepathadvisory.AppendRecord(path, record); err != nil {
		stage := "registry-save"
		if errors.Is(err, failurepathadvisory.ErrCohortFull) {
			stage = "cohort-cap"
		}
		w.recordFailurePathMeasurementFailure(stage, fmt.Errorf("outcome=%s call_id=%s: %w", record.Outcome, record.CallID, err))
		return err
	}
	return nil
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

func (w *Workflow) classifyFailurePathTrigger() (failurepathadvisory.TriggerDecision, error) {
	baselineHead := w.state.ReadOr("baseline-head", "")
	paths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
	if err != nil {
		return failurepathadvisory.TriggerDecision{}, err
	}
	return failurepathadvisory.ClassifyTrigger(w.config.RepoRoot, baselineHead, paths)
}

func (w *Workflow) runFailurePathAdvisoryReviewer(
	taskID string,
	request string,
	reviewResult packet.Result,
	reviewNumber int,
	trigger failurepathadvisory.TriggerDecision,
	diffText string,
) failurepathadvisory.Record {
	phase := fmt.Sprintf("failure-path-reviewer-%d", reviewNumber)
	model := w.config.HighRiskReviewerModel
	advisoryRunner, available := w.runner.(failurePathAdvisoryRunner)
	if !available {
		return w.newFailurePathAdvisoryRecord(
			taskID, reviewNumber, phase, trigger, failurepathadvisory.OutcomeMissingRunUnavailable, nil, "",
			"runnerがdeadline付きadvisory呼出に対応していません",
		)
	}
	prompt := failurePathAdvisoryPrompt(request, reviewResult, reviewNumber, trigger, diffText)
	outputPath := filepath.Join(w.temp, phase+".log")
	startedAt := w.now().UTC()
	result, runErr := advisoryRunner.RunWithDeadline(
		state.FailurePathReviewerRole, phase, model, true, w.config.RoutineEffort, prompt, outputPath,
		startedAt.Add(failurePathAdvisoryCallDeadline),
	)
	completedAt := w.now().UTC()
	outcome, detail := resolveFailurePathAdvisoryOutcome(runErr)
	findings := []failurepathadvisory.Finding{}
	if outcome == failurepathadvisory.OutcomeObserved {
		parsed, parseErr := failurepathadvisory.ParseStructuredOutput(result.StructuredOutput)
		if parseErr != nil {
			outcome = failurepathadvisory.OutcomeMissingSchemaInvalid
			detail = parseErr.Error()
		} else {
			findings = parsed
		}
	}
	callID := w.recordFailurePathAdvisoryCall(phase, prompt, model, result, startedAt, completedAt, outcome, runErr, outputPath)
	record := w.newFailurePathAdvisoryRecord(taskID, reviewNumber, phase, trigger, outcome, findings, callID, detail)
	record.ReviewPacketStatus = string(reviewResult.Status)
	record.ReviewIssues = boundedText(reviewResult.Issues, failurePathAdvisoryDetailBoundBytes)
	record.ReviewTargets = reviewResult.Targets
	record.AddedGLM = failurePathAdvisoryUsage(result, startedAt, completedAt)
	return record
}

func resolveFailurePathAdvisoryOutcome(runErr error) (string, string) {
	if runErr == nil {
		return failurepathadvisory.OutcomeObserved, ""
	}
	var interrupted *runner.InterruptedCallError
	switch {
	case errors.As(runErr, &interrupted):
		return failurepathadvisory.OutcomeMissingInterrupted, runErr.Error()
	case errors.Is(runErr, runner.ErrProbeDeadlineExceeded):
		return failurepathadvisory.OutcomeMissingDeadline, runErr.Error()
	case runner.IsStructuredOutputError(runErr):
		return failurepathadvisory.OutcomeMissingPartial, runErr.Error()
	default:
		return failurepathadvisory.OutcomeMissingProvider, runErr.Error()
	}
}

func (w *Workflow) recordFailurePathAdvisoryCall(
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

func failurePathAdvisoryUsage(result runner.RunResult, startedAt, completedAt time.Time) *failurepathadvisory.AddedGLMUsage {
	return &failurepathadvisory.AddedGLMUsage{
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

func (w *Workflow) newFailurePathAdvisoryRecord(
	taskID string,
	reviewNumber int,
	phase string,
	trigger failurepathadvisory.TriggerDecision,
	outcome string,
	findings []failurepathadvisory.Finding,
	callID string,
	detail string,
) failurepathadvisory.Record {
	return failurepathadvisory.Record{
		TaskID:           taskID,
		ReviewNumber:     reviewNumber,
		Phase:            phase,
		Outcome:          outcome,
		Classes:          trigger.Classes,
		AmbiguousClasses: trigger.AmbiguousClasses,
		TriggerPaths:     trigger.TriggerPaths,
		CallID:           callID,
		Findings:         findings,
		Detail:           boundedText(detail, failurePathAdvisoryDetailBoundBytes),
		RecordedAt:       w.now().UTC().Format(time.RFC3339),
	}
}

func failurePathAdvisoryPrompt(request string, reviewResult packet.Result, reviewNumber int, trigger failurepathadvisory.TriggerDecision, diffText string) string {
	prompt := fmt.Sprintf(`FAILURE_PATH_ADVISORY_MODE: PRODUCTION_ADVISORY

USER_REQUEST:
%s

TRIGGER_CLASSES:
%s

TRIGGER_PATHS:
%s

CHANGED_DIFF:
%s

REVIEW_NUMBER: %d

REVIEW_STATUS: %s

REVIEW_ISSUES:
%s

上記classとpathに限定して今回の変更の失敗経路を検証してください。CHANGED_DIFFはtrigger対象pathのtask snapshot対比変更hunk、USER_REQUEST・TRIGGER_PATHS・CHANGED_DIFF・REVIEW_ISSUESはbyte boundで切詰められており、冒頭の[前方を省略]は切詰め表示です。status=findingの検証済みfindingは、通常reviewer結果とは独立した短いadvisoryとしてSol-visible review packetへ機械的に掲載されます。advisoryはSol Highが検証する候補であり、通常reviewのstatus、accept、fix、routingを変更しません。
`,
		boundedText(strings.TrimSpace(request), failurePathAdvisoryRequestBoundBytes),
		boundedText(strings.Join(trigger.Classes, "\n"), failurePathAdvisoryClassesBoundBytes),
		boundedText(strings.Join(trigger.TriggerPaths, "\n"), failurePathAdvisoryTriggerPathsBoundBytes),
		boundedText(diffText, failurePathAdvisoryDiffBoundBytes),
		reviewNumber,
		reviewResult.Status,
		boundedText(reviewResult.Issues, failurePathAdvisoryReviewIssuesBoundBytes),
	)
	return boundedText(prompt, failurePathAdvisoryPromptMaxBytes)
}
