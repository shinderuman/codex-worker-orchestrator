package app

import (
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentUsageReport struct {
	Version       int                  `json:"version"`
	TaskID        string               `json:"task_id"`
	TaskStatus    string               `json:"task_status"`
	GeneratedAt   string               `json:"generated_at"`
	ParentSession parentUsageParent    `json:"parent_session"`
	Intervals     parentUsageIntervals `json:"intervals"`
}

type parentUsageParent struct {
	ThreadID         string `json:"thread_id,omitempty"`
	Status           string `json:"status"`
	AssociationBasis string `json:"association_basis,omitempty"`
	RolloutSource    string `json:"rollout_source,omitempty"`
	Detail           string `json:"detail,omitempty"`
}

type parentUsageIntervals struct {
	TaskExecution      parentUsageInterval `json:"task_execution"`
	ParentFinalization parentUsageInterval `json:"parent_finalization"`
}

type parentUsageInterval struct {
	Status   string              `json:"status"`
	Start    *string             `json:"start"`
	End      *string             `json:"end"`
	EndBasis string              `json:"end_basis,omitempty"`
	Tokens   parentUsageTokens   `json:"tokens"`
	Activity parentUsageActivity `json:"activity"`
}

type parentUsageTokens struct {
	Status            string                    `json:"status"`
	Reason            string                    `json:"reason,omitempty"`
	InputTokens       int64                     `json:"input_tokens,omitempty"`
	CachedInputTokens int64                     `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64                     `json:"output_tokens,omitempty"`
	ReasoningTokens   int64                     `json:"reasoning_output_tokens,omitempty"`
	TotalTokens       int64                     `json:"total_tokens,omitempty"`
	BaselineAt        string                    `json:"baseline_at,omitempty"`
	EndAt             string                    `json:"end_at,omitempty"`
	BaselineSource    string                    `json:"baseline_source,omitempty"`
	EndSource         string                    `json:"end_source,omitempty"`
	UnknownFields     []parentUsageUnknownField `json:"unknown_fields,omitempty"`
}

type parentUsageUnknownField struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Source string `json:"source"`
}

type parentUsageActivity struct {
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	ModelTurns      int    `json:"model_turns"`
	ToolCalls       int    `json:"tool_calls"`
	ToolResults     int    `json:"tool_results"`
	Compactions     int    `json:"compactions"`
	ToolOutputBytes int64  `json:"tool_output_bytes"`
	Source          string `json:"source,omitempty"`
}

const parentUsageReportVersion = 1

const parentUsageFieldInput = "input_tokens"

const parentUsageFieldCached = "cached_input_tokens"

const parentUsageFieldOutput = "output_tokens"

const parentUsageFieldReasoning = "reasoning_output_tokens"

const parentUsageFieldTotal = "total_tokens"

const parentUsageReasonExecutionBoundary = "execution-boundary-unknown"

const parentUsageReasonFinalizationInterval = "finalization-interval-unavailable"

const parentUsageReasonBaselineAnchor = "baseline-anchor-missing"

const parentUsageReasonEndAnchor = "end-anchor-missing"

const parentUsageReasonMissingInBaseline = "missing-in-baseline-anchor"

const parentUsageReasonMissingInEnd = "missing-in-end-anchor"

const parentUsageReasonRolloutUnreadable = "rollout-scan-failed"

const parentUsageIntervalStartInclusive = false

const parentUsageIntervalStartExclusive = true

func printParentUsage(cfg config.AppConfig, st *state.StateStore, requestedTaskID string, stdout io.Writer) error {
	task, err := selectBundleTask(st, requestedTaskID)
	if err != nil {
		return err
	}
	return writeJSON(stdout, buildParentUsageReport(cfg, st, task))
}

func buildParentUsageReport(cfg config.AppConfig, st *state.StateStore, task bundleTask) parentUsageReport {
	start, collectionEnd, _ := analysisCollectionWindow(task)
	execution := resolveAnalysisExecutionBoundary(st, task.ID)
	association := resolveCodexAssociation(cfg.CodexConfigDir, task)
	scan, scanErr := parentUsageRolloutScan(association, start, collectionEnd)
	ownership := resolveAnalysisTaskOwnership(association, scan.turns, start, collectionEnd, task.ID)
	finalization := analysisTaskFinalizationInterval(execution, ownership)
	return parentUsageReport{
		Version:       parentUsageReportVersion,
		TaskID:        task.ID,
		TaskStatus:    task.Status,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		ParentSession: parentUsageParentSession(association),
		Intervals: parentUsageIntervals{
			TaskExecution:      parentUsageExecutionInterval(association, scan, scanErr, start, execution, collectionEnd),
			ParentFinalization: parentUsageFinalizationInterval(association, scan, scanErr, execution, ownership, finalization),
		},
	}
}

func parentUsageParentSession(association codexAssociation) parentUsageParent {
	parent := parentUsageParent{Status: association.ParentStatus, Detail: association.Detail}
	if association.ParentStatus != codexStatusIncluded {
		return parent
	}
	parent.ThreadID = association.ParentThreadID
	parent.AssociationBasis = association.Basis
	parent.RolloutSource = association.ParentSource
	return parent
}

func parentUsageRolloutScan(association codexAssociation, start, end time.Time) (bundleRolloutScan, error) {
	if association.ParentStatus != codexStatusIncluded {
		return bundleRolloutScan{}, nil
	}
	scan, err := scanCodexRolloutWindow(association.ParentPath, start, end)
	if err != nil {
		return bundleRolloutScan{}, err
	}
	return scan, nil
}

func parentUsageExecutionInterval(association codexAssociation, scan bundleRolloutScan, scanErr error, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) parentUsageInterval {
	interval := parentUsageInterval{
		Status:   execution.status,
		Start:    analysisTimestamp(start),
		Tokens:   parentUsageTokens{Status: execution.status},
		Activity: parentUsageActivity{Status: execution.status},
	}
	if execution.status == analysisStatusAvailable {
		interval.End = analysisTimestamp(execution.end)
		interval.EndBasis = execution.endBasis
	}
	if association.ParentStatus != codexStatusIncluded {
		return parentUsageDegradedEvidence(interval, association.ParentStatus)
	}
	if scanErr != nil {
		return parentUsageUnreadableEvidence(interval, association.ParentSource)
	}
	if execution.status == analysisStatusUnknown {
		interval.Tokens.Reason = parentUsageReasonExecutionBoundary
		interval.Activity.Reason = parentUsageReasonExecutionBoundary
		return interval
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	interval.Tokens = parentUsageAnchoredTokens(scan, start, endBound, association.ParentSource)
	interval.Activity = parentUsageIntervalActivity(scan, start, endBound, association.ParentSource, parentUsageIntervalStartInclusive)
	if execution.status == analysisStatusOpen && interval.Tokens.Status == analysisStatusAvailable {
		interval.Tokens.Status = analysisStatusOpen
	}
	if execution.status == analysisStatusOpen && interval.Activity.Status == analysisStatusCounted {
		interval.Activity.Status = analysisStatusOpen
	}
	return interval
}

func parentUsageFinalizationInterval(association codexAssociation, scan bundleRolloutScan, scanErr error, execution analysisExecutionBoundary, ownership analysisTaskOwnership, interval bundleAnalysisInterval) parentUsageInterval {
	report := parentUsageInterval{
		Status:   interval.Status,
		Start:    interval.Start,
		End:      interval.End,
		EndBasis: interval.EndBasis,
		Tokens:   parentUsageTokens{Status: interval.Status},
		Activity: parentUsageActivity{Status: interval.Status},
	}
	if association.ParentStatus != codexStatusIncluded {
		return parentUsageDegradedEvidence(report, association.ParentStatus)
	}
	if scanErr != nil {
		return parentUsageUnreadableEvidence(report, association.ParentSource)
	}
	if interval.Status != analysisStatusAvailable || ownership.final == nil {
		if interval.Status == analysisStatusUnknown {
			report.Tokens.Reason = parentUsageReasonFinalizationInterval
			report.Activity.Reason = parentUsageReasonFinalizationInterval
		}
		return report
	}
	report.Tokens = parentUsageAnchoredTokens(scan, execution.end, ownership.final.CompletedAt, association.ParentSource)
	report.Activity = parentUsageIntervalActivity(scan, execution.end, ownership.final.CompletedAt, association.ParentSource, parentUsageIntervalStartExclusive)
	return report
}

func parentUsageDegradedEvidence(interval parentUsageInterval, status string) parentUsageInterval {
	interval.Tokens = parentUsageTokens{Status: status}
	interval.Activity = parentUsageActivity{Status: status}
	return interval
}

func parentUsageUnreadableEvidence(interval parentUsageInterval, source string) parentUsageInterval {
	interval.Tokens = parentUsageTokens{Status: analysisStatusUnreadable, Reason: parentUsageReasonRolloutUnreadable}
	interval.Activity = parentUsageActivity{Status: analysisStatusUnreadable, Reason: parentUsageReasonRolloutUnreadable, Source: source}
	return interval
}

func parentUsageAnchoredTokens(scan bundleRolloutScan, baselineBound, endBound time.Time, source string) parentUsageTokens {
	baseline, hasBaseline := lastTokenAnchorAtOrBefore(scan, baselineBound)
	end, hasEnd := lastTokenAnchorAtOrBefore(scan, endBound)
	switch {
	case !hasBaseline:
		return parentUsageTokens{Status: analysisStatusMissing, Reason: parentUsageReasonBaselineAnchor, BaselineSource: source}
	case !hasEnd:
		return parentUsageTokens{Status: analysisStatusMissing, Reason: parentUsageReasonEndAnchor, BaselineAt: baseline.RawAt, BaselineSource: parentUsageSourceLocator(source, baseline.Line)}
	case end.Offset <= baseline.Offset:
		return parentUsageTokens{Status: analysisStatusNoObservation, BaselineAt: baseline.RawAt, BaselineSource: parentUsageSourceLocator(source, baseline.Line)}
	case analysisCountersResetBetween(scan, baseline, end):
		return parentUsageTokens{
			Status:         analysisStatusCounterReset,
			BaselineAt:     baseline.RawAt,
			EndAt:          end.RawAt,
			BaselineSource: parentUsageSourceLocator(source, baseline.Line),
			EndSource:      parentUsageSourceLocator(source, end.Line),
		}
	}
	tokens := parentUsageTokens{
		Status:         analysisStatusAvailable,
		BaselineAt:     baseline.RawAt,
		EndAt:          end.RawAt,
		BaselineSource: parentUsageSourceLocator(source, baseline.Line),
		EndSource:      parentUsageSourceLocator(source, end.Line),
	}
	tokens.InputTokens, tokens.UnknownFields = parentUsageCounterField(parentUsageFieldInput, baseline.Input, end.Input, source, baseline, end, tokens.UnknownFields)
	tokens.CachedInputTokens, tokens.UnknownFields = parentUsageCounterField(parentUsageFieldCached, baseline.Cached, end.Cached, source, baseline, end, tokens.UnknownFields)
	tokens.OutputTokens, tokens.UnknownFields = parentUsageCounterField(parentUsageFieldOutput, baseline.Output, end.Output, source, baseline, end, tokens.UnknownFields)
	tokens.ReasoningTokens, tokens.UnknownFields = parentUsageCounterField(parentUsageFieldReasoning, baseline.Reasoning, end.Reasoning, source, baseline, end, tokens.UnknownFields)
	tokens.TotalTokens, tokens.UnknownFields = parentUsageCounterField(parentUsageFieldTotal, baseline.Total, end.Total, source, baseline, end, tokens.UnknownFields)
	return tokens
}

func parentUsageCounterField(field string, baseline, end *int64, source string, baselineAnchor, endAnchor analysisRolloutTokenAnchor, unknowns []parentUsageUnknownField) (int64, []parentUsageUnknownField) {
	delta := analysisCounterDeltaState(baseline, end)
	if delta.Known {
		return delta.Value, unknowns
	}
	reason := parentUsageReasonMissingInEnd
	missing := endAnchor
	if delta.MissingInBaseline {
		reason = parentUsageReasonMissingInBaseline
		missing = baselineAnchor
	}
	return 0, append(unknowns, parentUsageUnknownField{
		Field:  field,
		Reason: reason,
		Source: parentUsageSourceLocator(source, missing.Line),
	})
}

func parentUsageIntervalActivity(scan bundleRolloutScan, start, end time.Time, source string, startExclusive bool) parentUsageActivity {
	activity := parentUsageActivity{Status: analysisStatusCounted, Source: source}
	if !scan.hasWindow {
		activity.Status = analysisStatusNoObservation
		return activity
	}
	activity.ModelTurns, activity.ToolCalls, activity.ToolResults, activity.Compactions, activity.ToolOutputBytes = parentUsageRolloutActivity(scan, start, end, startExclusive)
	return activity
}

func parentUsageRolloutActivity(scan bundleRolloutScan, start, end time.Time, startExclusive bool) (modelTurns, toolCalls, toolResults, compactions int, outputBytes int64) {
	for i := range scan.turns {
		if parentUsageIntervalCountsTurn(&scan.turns[i], start, end, startExclusive) {
			modelTurns++
		}
	}
	for _, event := range scan.toolEvents {
		if !parentUsageEventWithinInterval(event.At, start, end, startExclusive) {
			continue
		}
		if event.Call {
			toolCalls++
			continue
		}
		toolResults++
		outputBytes += event.OutputBytes
	}
	for _, compaction := range scan.compactions {
		if parentUsageEventWithinInterval(compaction.At, start, end, startExclusive) {
			compactions++
		}
	}
	return modelTurns, toolCalls, toolResults, compactions, outputBytes
}

func parentUsageEventWithinInterval(at, start, end time.Time, startExclusive bool) bool {
	if at.After(end) {
		return false
	}
	if startExclusive {
		return at.After(start)
	}
	return !at.Before(start)
}

func parentUsageIntervalCountsTurn(turn *analysisRolloutTurn, start, end time.Time, startExclusive bool) bool {
	if !turn.HasStart || turn.StartedAt.After(end) {
		return false
	}
	if startExclusive {
		return turn.StartedAt.After(start)
	}
	return !turn.HasComplete || !turn.CompletedAt.Before(start)
}

func parentUsageSourceLocator(source string, line int) string {
	return fmt.Sprintf("%s:%d", source, line)
}
