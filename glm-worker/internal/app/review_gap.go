package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type reviewGapReport struct {
	Version     int             `json:"version"`
	Scope       string          `json:"scope"`
	TaskID      string          `json:"task_id,omitempty"`
	TaskCount   int             `json:"task_count"`
	GeneratedAt string          `json:"generated_at"`
	FixCount    int             `json:"fix_count"`
	Summary     reviewGapCounts `json:"summary"`
	Fixes       []reviewGapFix  `json:"fixes"`
	Coverage    string          `json:"coverage"`
}

type reviewGapCounts struct {
	ByOrigin      map[string]int `json:"by_origin,omitempty"`
	ByCause       map[string]int `json:"by_cause,omitempty"`
	ByCauseStatus map[string]int `json:"by_cause_status,omitempty"`
	ByCategory    map[string]int `json:"by_category,omitempty"`
	BySemanticity map[string]int `json:"by_semanticity,omitempty"`
}

type reviewGapFix struct {
	TaskID               string                  `json:"task_id"`
	FixEvent             reviewGapEventRef       `json:"fix_event"`
	Origin               string                  `json:"origin"`
	Cause                string                  `json:"cause,omitempty"`
	CauseStatus          string                  `json:"cause_status"`
	Round                *reviewGapRoundRef      `json:"round,omitempty"`
	Categories           []string                `json:"categories,omitempty"`
	CategoryStatus       string                  `json:"category_status"`
	CategoryReason       string                  `json:"category_reason,omitempty"`
	Semanticity          string                  `json:"semanticity,omitempty"`
	SemanticityStatus    string                  `json:"semanticity_status"`
	SemanticityReason    string                  `json:"semanticity_reason,omitempty"`
	ParentReworkInterval reviewGapReworkInterval `json:"parent_rework_interval"`
	Downstream           reviewGapDownstream     `json:"downstream"`
}

type reviewGapEventRef struct {
	CallID string `json:"call_id,omitempty"`
	At     string `json:"at"`
	Phase  string `json:"phase,omitempty"`
}

type reviewGapRoundRef struct {
	Seq          int    `json:"seq"`
	ReviewNumber int    `json:"review_number"`
	WorkerPhase  string `json:"worker_phase"`
}

type reviewGapReworkInterval struct {
	Status   string              `json:"status"`
	Reason   string              `json:"reason,omitempty"`
	From     reviewGapBoundary   `json:"from"`
	To       reviewGapBoundary   `json:"to"`
	Tokens   parentUsageTokens   `json:"tokens"`
	Activity parentUsageActivity `json:"activity"`
}

type reviewGapBoundary struct {
	Basis  string `json:"basis"`
	CallID string `json:"call_id,omitempty"`
	Phase  string `json:"phase,omitempty"`
	At     string `json:"at"`
}

type reviewGapDownstream struct {
	Status        string               `json:"status"`
	Reason        string               `json:"reason,omitempty"`
	WorkerCalls   []reviewGapCallRef   `json:"worker_fix_calls,omitempty"`
	ReviewerCalls []reviewGapCallRef   `json:"reviewer_calls,omitempty"`
	Rework        *reviewGapReworkCost `json:"rework,omitempty"`
}

type reviewGapCallRef struct {
	CallID       string `json:"call_id"`
	SessionID    string `json:"session_id,omitempty"`
	Phase        string `json:"phase"`
	ReviewNumber int    `json:"review_number,omitempty"`
}

type reviewGapReworkCost struct {
	WorkerCalls      int   `json:"worker_calls"`
	ReviewerCalls    int   `json:"reviewer_calls"`
	Turns            int   `json:"turns"`
	TreeInputTokens  int64 `json:"tree_input_tokens"`
	TreeOutputTokens int64 `json:"tree_output_tokens"`
	WallDurationMS   int64 `json:"wall_duration_ms"`
}

type reviewGapTaskEvidence struct {
	stats       *state.TaskStats
	logs        []state.ModelCallLog
	events      []state.ModelCallLog
	eventIndex  int
	rounds      []state.RoundRecord
	roundErr    error
	taskEvents  []state.TaskEventRecord
	association codexAssociation
	scan        bundleRolloutScan
	scanErr     error
}

const reviewGapReportVersion = 1

const reviewGapScopeTask = "task"

const reviewGapScopeAll = "all"

const reviewGapKnown = "known"

const reviewGapUnknown = "unknown"

const reviewGapCoverageComplete = "complete"

const reviewGapReasonFixRoundMissing = "fix-round-missing"

const reviewGapReasonRoundCaptureError = "round-capture-error"

const reviewGapReasonNoChangedPaths = "no-changed-paths"

const reviewGapReasonPreviousRoundMissing = "previous-round-missing"

const reviewGapReasonRoundLogMissing = "round-log-missing"

const reviewGapReasonRoundLogUnreadable = "round-log-unreadable"

const reviewGapReasonWorkerFixCallMissing = "worker-fix-call-missing"

const reviewGapReasonReviewerCallMissing = "reviewer-call-missing"

const reviewGapReasonRolloutScanFailed = "rollout-scan-failed"

const reviewGapBasisPreviousOutcome = "previous-parent-outcome"

const reviewGapBasisTaskStart = "task-start"

const reviewGapBasisFixDeclaration = "fix-declaration"

const reviewGapWorkerExplicitFixPhase = state.WorkerPhaseCategoryExplicitFix

func printReviewGap(cfg config.AppConfig, st *state.StateStore, requestedTaskID string, stdout io.Writer) error {
	tasks, scope, err := reviewGapTasks(st, requestedTaskID)
	if err != nil {
		return err
	}
	report := buildReviewGapReport(cfg, st, tasks, scope, requestedTaskID)
	return writeJSON(stdout, report)
}

func reviewGapTasks(st *state.StateStore, requestedTaskID string) ([]state.TaskStats, string, error) {
	all, err := st.AllTaskStats()
	if err != nil {
		return nil, "", fmt.Errorf("task statsを読めません: %w", err)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].TaskID < all[j].TaskID
		}
		return all[i].StartedAt.Before(all[j].StartedAt)
	})
	if requestedTaskID == "" {
		return all, reviewGapScopeAll, nil
	}
	currentID := st.ReadOr("task.id", "")
	if requestedTaskID == currentID {
		if stats, err := st.CurrentTaskStats(); err == nil {
			return []state.TaskStats{stats}, reviewGapScopeTask, nil
		}
	}
	for _, stats := range all {
		if stats.TaskID == requestedTaskID {
			return []state.TaskStats{stats}, reviewGapScopeTask, nil
		}
	}
	return nil, "", &NotFoundError{Message: fmt.Sprintf("task %sのretained evidenceがありません", requestedTaskID)}
}

func buildReviewGapReport(cfg config.AppConfig, st *state.StateStore, tasks []state.TaskStats, scope string, taskID string) reviewGapReport {
	report := reviewGapReport{
		Version:     reviewGapReportVersion,
		Scope:       scope,
		TaskID:      taskID,
		TaskCount:   len(tasks),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Summary:     reviewGapCounts{},
		Fixes:       make([]reviewGapFix, 0),
		Coverage:    reviewGapCoverageComplete,
	}
	for index := range tasks {
		report.appendReviewGapTask(cfg, st, &tasks[index])
	}
	report.FixCount = len(report.Fixes)
	report.Summary = reviewGapCountsOf(report.Fixes)
	return report
}

func reviewGapCountsOf(fixes []reviewGapFix) reviewGapCounts {
	counts := reviewGapCounts{}
	for _, fix := range fixes {
		reviewGapIncrement(&counts.ByOrigin, fix.Origin)
		reviewGapIncrement(&counts.ByCauseStatus, fix.CauseStatus)
		if fix.Cause != "" {
			reviewGapIncrement(&counts.ByCause, fix.Cause)
		}
		for _, category := range fix.Categories {
			reviewGapIncrement(&counts.ByCategory, category)
		}
		if fix.Semanticity != "" {
			reviewGapIncrement(&counts.BySemanticity, fix.Semanticity)
		}
	}
	return counts
}

func reviewGapIncrement(counts *map[string]int, key string) {
	if *counts == nil {
		*counts = make(map[string]int)
	}
	(*counts)[key]++
}

func (report *reviewGapReport) appendReviewGapTask(cfg config.AppConfig, st *state.StateStore, stats *state.TaskStats) {
	logs, logErr := st.ReadModelCallLogs(stats.TaskID)
	if logErr != nil && !reviewGapTelemetryAbsent(logErr, stats) {
		report.Coverage = state.ParentReworkCoverageUnknown
		return
	}
	events := reviewGapOutcomeEvents(logs)
	if len(events) == 0 {
		return
	}
	rounds, roundErr := st.ReadRoundRecords(stats.TaskID)
	taskEvents, _, _ := readTaskEventRecords(st, stats.TaskID)
	association := resolveCodexAssociation(cfg.CodexConfigDir, bundleTask{ID: stats.TaskID, Status: string(stats.Status), Stats: *stats})
	windowStart, windowEnd, _ := analysisCollectionWindow(bundleTask{Stats: *stats})
	scan, scanErr := parentUsageRolloutScan(association, windowStart, windowEnd)
	for index := range events {
		if events[index].Outcome == state.ParentOutcomeFix {
			report.Fixes = append(report.Fixes, buildReviewGapFix(
				reviewGapTaskEvidence{
					stats: stats, logs: logs, events: events, eventIndex: index,
					rounds: rounds, roundErr: roundErr, taskEvents: taskEvents,
					association: association, scan: scan, scanErr: scanErr,
				},
			))
		}
	}
}

func reviewGapTelemetryAbsent(err error, stats *state.TaskStats) bool {
	return errors.Is(err, os.ErrNotExist) && stats.ModelCalls == 0
}

func reviewGapOutcomeEvents(logs []state.ModelCallLog) []state.ModelCallLog {
	events := make([]state.ModelCallLog, 0)
	for _, record := range logs {
		if record.CallType == state.CallTypeEvent && reviewGapIsOutcomePhase(record.Phase) {
			events = append(events, record)
		}
	}
	return events
}

func reviewGapIsOutcomePhase(phase string) bool {
	switch phase {
	case state.ParentPhaseAccept, state.ParentPhaseFix, state.ParentPhaseDecision, state.ParentPhaseClose:
		return true
	}
	return false
}

func buildReviewGapFix(evidence reviewGapTaskEvidence) reviewGapFix {
	event := evidence.events[evidence.eventIndex]
	origin := event.ParentOrigin
	if origin == "" {
		origin = state.ParentOriginUnknown
	}
	fix := reviewGapFix{
		TaskID:      evidence.stats.TaskID,
		FixEvent:    reviewGapEventRefOf(event),
		Origin:      origin,
		Cause:       event.ParentCause,
		CauseStatus: reviewGapCauseStatus(event, origin),
	}
	reviewGapFillRound(&fix, evidence)
	fix.ParentReworkInterval = buildReviewGapReworkInterval(evidence)
	fix.Downstream = buildReviewGapDownstream(evidence)
	return fix
}

func reviewGapCauseStatus(event state.ModelCallLog, origin string) string {
	if event.ParentCause != "" {
		return reviewGapKnown
	}
	if origin == state.ParentOriginCodexReview {
		return state.CauseStatusMissingLegacy
	}
	return state.CauseStatusNotDeclared
}

func reviewGapEventRefOf(event state.ModelCallLog) reviewGapEventRef {
	return reviewGapEventRef{CallID: event.CallID, At: event.StartedAt.UTC().Format(time.RFC3339Nano), Phase: event.Phase}
}

func reviewGapFillRound(fix *reviewGapFix, evidence reviewGapTaskEvidence) {
	fix.CategoryStatus = reviewGapUnknown
	fix.SemanticityStatus = reviewGapUnknown
	if evidence.roundErr != nil {
		reason := reviewGapReasonRoundLogUnreadable
		if errors.Is(evidence.roundErr, os.ErrNotExist) {
			reason = reviewGapReasonRoundLogMissing
		}
		fix.CategoryReason = reason
		fix.SemanticityReason = reason
		return
	}
	round, previous, found := reviewGapFixRound(evidence)
	if !found {
		fix.CategoryReason = reviewGapReasonFixRoundMissing
		fix.SemanticityReason = reviewGapReasonFixRoundMissing
		return
	}
	fix.Round = &reviewGapRoundRef{Seq: round.Seq, ReviewNumber: round.ReviewNumber, WorkerPhase: round.WorkerPhase}
	reviewGapFillCategories(fix, previous, round)
	reviewGapFillSemanticity(fix, evidence, previous, round)
}

func reviewGapFixRound(evidence reviewGapTaskEvidence) (state.RoundRecord, *state.RoundRecord, bool) {
	fixAt := evidence.events[evidence.eventIndex].StartedAt
	windowEnd := reviewGapNextOutcomeAt(evidence)
	for index := range evidence.rounds {
		round := &evidence.rounds[index]
		if round.WorkerPhase != reviewGapWorkerExplicitFixPhase || round.CapturedAt.Before(fixAt) {
			continue
		}
		if !windowEnd.IsZero() && !round.CapturedAt.Before(windowEnd) {
			continue
		}
		return *round, reviewGapPreviousRound(evidence.rounds, round.Seq), true
	}
	return state.RoundRecord{}, nil, false
}

func reviewGapPreviousRound(rounds []state.RoundRecord, seq int) *state.RoundRecord {
	for index := range rounds {
		if rounds[index].Seq == seq-1 {
			previous := rounds[index]
			return &previous
		}
	}
	return nil
}

func reviewGapNextOutcomeAt(evidence reviewGapTaskEvidence) time.Time {
	if evidence.eventIndex+1 < len(evidence.events) {
		return evidence.events[evidence.eventIndex+1].StartedAt
	}
	return time.Time{}
}

func reviewGapFillCategories(fix *reviewGapFix, previous *state.RoundRecord, round state.RoundRecord) {
	if round.CaptureError != "" || (previous != nil && previous.CaptureError != "") {
		fix.CategoryReason = reviewGapReasonRoundCaptureError
		return
	}
	changed := reviewGapChangedPaths(previous, round)
	if len(changed) == 0 {
		fix.CategoryReason = reviewGapReasonNoChangedPaths
		return
	}
	categories := map[string]bool{}
	for _, path := range changed {
		categories[state.FixPathCategory(path)] = true
	}
	fix.Categories = make([]string, 0, len(categories))
	for category := range categories {
		fix.Categories = append(fix.Categories, category)
	}
	sort.Strings(fix.Categories)
	fix.CategoryStatus = reviewGapKnown
}

func reviewGapChangedPaths(previous *state.RoundRecord, round state.RoundRecord) []string {
	current := reviewGapPathIndex(round.Paths)
	var beforeIndex map[string]state.RoundPathState
	if previous != nil {
		beforeIndex = reviewGapPathIndex(previous.Paths)
	}
	changed := make([]string, 0)
	for path := range beforeIndex {
		if _, kept := current[path]; !kept {
			changed = append(changed, path)
		}
	}
	for _, entry := range round.Paths {
		before, existed := beforeIndex[entry.Path]
		if existed && before.FullDigest == entry.FullDigest && entry.FullDigest != "" {
			continue
		}
		changed = append(changed, entry.Path)
	}
	sort.Strings(changed)
	return changed
}

func reviewGapPathIndex(paths []state.RoundPathState) map[string]state.RoundPathState {
	index := make(map[string]state.RoundPathState, len(paths))
	for _, entry := range paths {
		index[entry.Path] = entry
	}
	return index
}

func reviewGapFillSemanticity(fix *reviewGapFix, evidence reviewGapTaskEvidence, previous *state.RoundRecord, round state.RoundRecord) {
	if round.CaptureError != "" || (previous != nil && previous.CaptureError != "") {
		fix.SemanticityReason = reviewGapReasonRoundCaptureError
		return
	}
	if previous == nil {
		fix.SemanticityReason = reviewGapReasonPreviousRoundMissing
		return
	}
	delta := state.CompareRoundRecords(previous, &round)
	if delta.Class == state.RoundDeltaUnknown {
		fix.SemanticityReason = reviewGapReasonNoChangedPaths
		return
	}
	class := delta.Class
	if class == state.RoundDeltaSameSnapshot {
		uses, mutating := convergenceWorkerToolUse(evidence.taskEvents, round.WorkerPhase)
		if uses > 0 && !mutating {
			class = convergenceDeltaVerificationOnly
		}
	}
	fix.Semanticity = class
	fix.SemanticityStatus = reviewGapKnown
}

func buildReviewGapReworkInterval(evidence reviewGapTaskEvidence) reviewGapReworkInterval {
	event := evidence.events[evidence.eventIndex]
	interval := reviewGapReworkInterval{
		To: reviewGapBoundary{
			Basis: reviewGapBasisFixDeclaration, CallID: event.CallID,
			Phase: event.Phase, At: event.StartedAt.UTC().Format(time.RFC3339Nano),
		},
	}
	if evidence.eventIndex > 0 {
		previous := evidence.events[evidence.eventIndex-1]
		interval.From = reviewGapBoundary{
			Basis: reviewGapBasisPreviousOutcome, CallID: previous.CallID,
			Phase: previous.Phase, At: previous.StartedAt.UTC().Format(time.RFC3339Nano),
		}
	} else {
		interval.From = reviewGapBoundary{
			Basis: reviewGapBasisTaskStart,
			At:    evidence.stats.StartedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	fromAt := evidence.stats.StartedAt
	if evidence.eventIndex > 0 {
		fromAt = evidence.events[evidence.eventIndex-1].StartedAt
	}
	if evidence.association.ParentStatus != codexStatusIncluded {
		interval.Status = evidence.association.ParentStatus
		interval.Reason = "parent-session-" + evidence.association.ParentStatus
		return interval
	}
	if evidence.scanErr != nil {
		interval.Status = analysisStatusUnreadable
		interval.Reason = reviewGapReasonRolloutScanFailed
		interval.Activity = parentUsageActivity{Status: analysisStatusUnreadable, Reason: reviewGapReasonRolloutScanFailed, Source: evidence.association.ParentSource}
		return interval
	}
	interval.Tokens = parentUsageAnchoredTokens(evidence.scan, fromAt, event.StartedAt, evidence.association.ParentSource)
	interval.Activity = parentUsageIntervalActivity(evidence.scan, fromAt, event.StartedAt, evidence.association.ParentSource, parentUsageIntervalStartExclusive)
	interval.Status = interval.Tokens.Status
	if interval.Activity.Status != analysisStatusCounted && interval.Tokens.Status == analysisStatusAvailable {
		interval.Status = interval.Activity.Status
	}
	return interval
}

func buildReviewGapDownstream(evidence reviewGapTaskEvidence) reviewGapDownstream {
	fixAt := evidence.events[evidence.eventIndex].StartedAt
	windowEnd := reviewGapNextOutcomeAt(evidence)
	downstream := reviewGapDownstream{Status: reviewGapKnown}
	cost := reviewGapReworkCost{}
	for _, record := range evidence.logs {
		if record.CallType != state.CallTypeTask || record.StartedAt.Before(fixAt) {
			continue
		}
		if !windowEnd.IsZero() && !record.StartedAt.Before(windowEnd) {
			continue
		}
		if reviewGapCollectDownstreamCall(&downstream, &cost, record) {
			continue
		}
	}
	switch {
	case len(downstream.WorkerCalls) == 0:
		downstream.Status = reviewGapUnknown
		downstream.Reason = reviewGapReasonWorkerFixCallMissing
	case len(downstream.ReviewerCalls) == 0:
		downstream.Status = reviewGapUnknown
		downstream.Reason = reviewGapReasonReviewerCallMissing
	}
	if len(downstream.WorkerCalls) > 0 {
		cost.WorkerCalls = len(downstream.WorkerCalls)
		cost.ReviewerCalls = len(downstream.ReviewerCalls)
		downstream.Rework = &cost
	}
	return downstream
}

func reviewGapCollectDownstreamCall(downstream *reviewGapDownstream, cost *reviewGapReworkCost, record state.ModelCallLog) bool {
	switch record.Role {
	case state.WorkerRole:
		if record.Phase != reviewGapWorkerExplicitFixPhase && record.Phase != reviewGapWorkerExplicitFixPhase+"-result-correct" {
			return false
		}
		downstream.WorkerCalls = append(downstream.WorkerCalls, reviewGapCallRefOf(record))
	case state.ReviewerRole:
		if _, ok := state.ParseReviewerPhase(record.Phase); !ok {
			return false
		}
		downstream.ReviewerCalls = append(downstream.ReviewerCalls, reviewGapCallRefOf(record))
	default:
		return false
	}
	cost.Turns += record.TopLevelTurns
	cost.TreeInputTokens += record.TreeUsage.InputTokens +
		record.TreeUsage.CacheCreationInputTokens + record.TreeUsage.CacheReadInputTokens
	cost.TreeOutputTokens += record.TreeUsage.OutputTokens
	cost.WallDurationMS += record.WallDurationMS
	return true
}

func reviewGapCallRefOf(record state.ModelCallLog) reviewGapCallRef {
	ref := reviewGapCallRef{CallID: record.CallID, SessionID: record.SessionID, Phase: record.Phase}
	if parsed, ok := state.ParseReviewerPhase(record.Phase); ok && record.Role == state.ReviewerRole {
		ref.ReviewNumber = parsed.ReviewNumber
	}
	return ref
}
