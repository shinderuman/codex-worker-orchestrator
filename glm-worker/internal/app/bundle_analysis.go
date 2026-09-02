package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type bundleAnalysisIndex struct {
	Version        int                       `json:"version"`
	TaskID         string                    `json:"task_id"`
	TaskStatus     string                    `json:"task_status"`
	GeneratedAt    string                    `json:"generated_at"`
	Intervals      bundleAnalysisIntervals   `json:"intervals"`
	ParentSession  bundleAnalysisParent      `json:"parent_session"`
	RolloutWindow  bundleAnalysisRollout     `json:"parent_rollout_window"`
	WaitCalls      bundleAnalysisWaitCalls   `json:"parent_wait_calls"`
	TokenDelta     bundleAnalysisTokenDelta  `json:"parent_token_delta"`
	Finalization   bundleAnalysisTokenDelta  `json:"parent_finalization"`
	ValidationRuns bundleAnalysisValidations `json:"validation_runs"`
	Retries        bundleAnalysisRetries     `json:"retries"`
	Evidence       bundleAnalysisEvidence    `json:"evidence"`
}

type bundleAnalysisIntervals struct {
	TaskExecution      bundleAnalysisInterval    `json:"task_execution"`
	ParentFinalization bundleAnalysisInterval    `json:"parent_finalization"`
	SubsequentRequests bundleAnalysisSubsequents `json:"subsequent_requests"`
	Collection         bundleAnalysisInterval    `json:"collection"`
}

type bundleAnalysisInterval struct {
	Status   string  `json:"status"`
	Start    *string `json:"start"`
	End      *string `json:"end"`
	EndBasis string  `json:"end_basis,omitempty"`
}

type bundleAnalysisSubsequents struct {
	Status      string                         `json:"status"`
	Attribution string                         `json:"attribution"`
	Turns       []bundleAnalysisSubsequentTurn `json:"turns,omitempty"`
}

type bundleAnalysisSubsequentTurn struct {
	TurnID            string  `json:"turn_id"`
	Status            string  `json:"status"`
	StartedAt         string  `json:"started_at"`
	CompletedAt       *string `json:"completed_at"`
	InputTokens       int64   `json:"input_tokens,omitempty"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	BaselineAt        string  `json:"baseline_at,omitempty"`
	EndAt             string  `json:"end_at,omitempty"`
}

type bundleAnalysisParent struct {
	ThreadID           string `json:"thread_id,omitempty"`
	Status             string `json:"status"`
	AssociationBasis   string `json:"association_basis,omitempty"`
	RolloutArchivePath string `json:"rollout_archive_path,omitempty"`
	Detail             string `json:"detail,omitempty"`
}

type bundleAnalysisRollout struct {
	Status            string `json:"status"`
	TotalBytes        int64  `json:"total_bytes,omitempty"`
	WindowStartOffset int64  `json:"window_start_offset,omitempty"`
	WindowEndOffset   int64  `json:"window_end_offset,omitempty"`
	WindowBytes       int64  `json:"window_bytes,omitempty"`
	BaselineOffset    int64  `json:"baseline_offset,omitempty"`
}

type bundleAnalysisCount struct {
	Status string `json:"status"`
	Count  int    `json:"count,omitempty"`
}

type bundleAnalysisTokenDelta struct {
	Status            string `json:"status"`
	InputTokens       int64  `json:"input_tokens,omitempty"`
	CachedInputTokens int64  `json:"cached_input_tokens,omitempty"`
	BaselineAt        string `json:"baseline_at,omitempty"`
	EndAt             string `json:"end_at,omitempty"`
}

type bundleAnalysisValidations struct {
	Status string              `json:"status"`
	Runs   []bundleAnalysisRun `json:"runs,omitempty"`
}

type bundleAnalysisRun struct {
	RunID       string   `json:"run_id"`
	ArchivePath string   `json:"archive_path"`
	Form        string   `json:"form,omitempty"`
	Result      string   `json:"result,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	StartedAt   string   `json:"started_at,omitempty"`
	CompletedAt string   `json:"completed_at,omitempty"`
	RoundSeq    int      `json:"round_seq,omitempty"`
	Attribution string   `json:"attribution"`
	Bases       []string `json:"bases"`
}

type bundleAnalysisRetries struct {
	ValidationReruns   []bundleAnalysisRerun        `json:"validation_reruns,omitempty"`
	WorkerCounters     map[string]int               `json:"worker_counters,omitempty"`
	ResumedModelCalls  bundleAnalysisCount          `json:"resumed_model_calls"`
	ModelCallRelations bundleAnalysisModelRelations `json:"model_call_relations"`
}

type bundleAnalysisModelRelations struct {
	Status           string                            `json:"status"`
	Resolved         []bundleAnalysisRetryEdge         `json:"resolved,omitempty"`
	Dangling         []bundleAnalysisRetryEdge         `json:"dangling,omitempty"`
	Ambiguous        []bundleAnalysisAmbiguousRelation `json:"ambiguous,omitempty"`
	Unlinked         []bundleAnalysisUnlinkedCall      `json:"unlinked,omitempty"`
	DuplicateCallIDs []bundleAnalysisDuplicateCalls    `json:"duplicate_call_ids,omitempty"`
}

type bundleAnalysisRetryEdge struct {
	CallID      string                    `json:"call_id"`
	RetryOf     string                    `json:"retry_of"`
	RetryReason string                    `json:"retry_reason,omitempty"`
	Phase       string                    `json:"phase,omitempty"`
	Outcome     string                    `json:"outcome,omitempty"`
	Resumed     bool                      `json:"resumed"`
	Source      bundleAnalysisRecordTrace `json:"source"`
}

type bundleAnalysisAmbiguousRelation struct {
	CallID      string                    `json:"call_id"`
	RetryOf     string                    `json:"retry_of,omitempty"`
	RetryReason string                    `json:"retry_reason,omitempty"`
	Phase       string                    `json:"phase,omitempty"`
	Outcome     string                    `json:"outcome,omitempty"`
	Resumed     bool                      `json:"resumed"`
	Ambiguity   []string                  `json:"ambiguity"`
	Source      bundleAnalysisRecordTrace `json:"source"`
}

type bundleAnalysisUnlinkedCall struct {
	CallID      string                    `json:"call_id"`
	Phase       string                    `json:"phase,omitempty"`
	Outcome     string                    `json:"outcome,omitempty"`
	Resumed     bool                      `json:"resumed"`
	RetryReason string                    `json:"retry_reason,omitempty"`
	Source      bundleAnalysisRecordTrace `json:"source"`
}

type bundleAnalysisDuplicateCalls struct {
	CallID string `json:"call_id"`
	Lines  []int  `json:"lines"`
}

type bundleAnalysisRecordTrace struct {
	ArchivePath string `json:"archive_path"`
	Lines       []int  `json:"lines"`
}

type bundleAnalysisWaitCalls struct {
	Status           string                        `json:"status"`
	Count            int                           `json:"count,omitempty"`
	Calls            []bundleAnalysisWaitCall      `json:"calls,omitempty"`
	DuplicateCallIDs []bundleAnalysisWaitDuplicate `json:"duplicate_call_ids,omitempty"`
}

type bundleAnalysisWaitCall struct {
	CallID           string   `json:"call_id,omitempty"`
	RequestedYieldMS *float64 `json:"requested_yield_ms,omitempty"`
	YieldClass       string   `json:"yield_class"`
	RequestLines     []int    `json:"request_lines"`
	ReturnLines      []int    `json:"return_lines,omitempty"`
}

type bundleAnalysisWaitDuplicate struct {
	CallID       string `json:"call_id"`
	RequestLines []int  `json:"request_lines"`
	ReturnLines  []int  `json:"return_lines"`
}

type bundleAnalysisRerun struct {
	RunID         string `json:"run_id"`
	Form          string `json:"form"`
	Reason        string `json:"reason"`
	PreviousRunID string `json:"previous_run_id,omitempty"`
}

type bundleAnalysisEvidence struct {
	Task          []bundleAnalysisEvidenceRef `json:"task,omitempty"`
	ParentSession []bundleAnalysisEvidenceRef `json:"parent_session,omitempty"`
	Unattributed  []bundleAnalysisEvidenceRef `json:"unattributed,omitempty"`
	TaskExternal  []bundleAnalysisEvidenceRef `json:"task_external,omitempty"`
}

type bundleAnalysisEvidenceRef struct {
	ArchivePath string `json:"archive_path"`
	Basis       string `json:"basis"`
}

type bundleRolloutScan struct {
	totalBytes  int64
	windowStart int64
	windowEnd   int64
	hasWindow   bool
	turns       []analysisRolloutTurn
	turnIndex   map[string]int
	tokens      []analysisRolloutTokenAnchor
	waits       []analysisRolloutWaitRequest
	waitReturns []analysisRolloutWaitReturn
}

type analysisRolloutWaitRequest struct {
	CallID  string
	Line    int
	At      time.Time
	YieldMS *float64
}

type analysisRolloutWaitReturn struct {
	CallID string
	Line   int
}

type analysisRolloutTurn struct {
	TurnID      string
	StartedAt   time.Time
	StartOffset int64
	HasStart    bool
	CompletedAt time.Time
	HasComplete bool
}

type analysisRolloutTokenAnchor struct {
	At     time.Time
	RawAt  string
	Offset int64
	Input  int64
	Cached int64
}

type codexRolloutScanLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexRolloutEventPayload struct {
	Type   string                    `json:"type"`
	TurnID string                    `json:"turn_id"`
	Info   *codexRolloutTokenPayload `json:"info"`
}

type codexRolloutTokenPayload struct {
	TotalTokenUsage *codexRolloutTokenUsage `json:"total_token_usage"`
}

type codexRolloutTokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
}

type codexRolloutItemPayload struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
}

type analysisValidationEvent struct {
	RunID  string
	Form   string
	Result string
	At     time.Time
}

type analysisExecutionBoundary struct {
	status   string
	end      time.Time
	endBasis string
}

type analysisOwningTurn struct {
	status string
	turn   *analysisRolloutTurn
}

const bundleAnalysisIndexVersion = 3

const bundleAnalysisEntryPath = "analysis-index.json"

const bundleAnalysisRunsArchivePrefix = "current-state/diagnostics/quality-gate-runs/"

const (
	analysisStatusAvailable     = "available"
	analysisStatusCounted       = "counted"
	analysisStatusMissing       = "missing"
	analysisStatusNoObservation = "no-observation"
	analysisStatusNotCollected  = "not-collected"
	analysisStatusCounterReset  = "counter-reset"
	analysisStatusUnreadable    = "unreadable"
	analysisStatusUnknown       = "unknown"
	analysisStatusOpen          = "open"
)

const (
	analysisAttributionTask            = "task"
	analysisAttributionWindowUnmatched = "task-window-unattributed"
	analysisAttributionExternal        = "task-external"
	analysisAttributionUnknown         = "unknown"
	analysisAttributionSubsequent      = "unattributed-subsequent-request"
)

const (
	analysisBasisTaskEventValidation   = "task-event-validation"
	analysisBasisRoundSnapshotDigest   = "round-snapshot-digest"
	analysisBasisWindowOverlap         = "window-overlap"
	analysisBasisOutsideWindow         = "outside-window"
	analysisBasisTaskScopedState       = "task-scoped-state-store"
	analysisBasisModelCallSession      = "model-call-log-session"
	analysisBasisCurrentState          = "bundle-time-current-state"
	analysisBasisParentRolloutWindow   = "parent-rollout-window"
	analysisBasisGuardianWindowOverlap = "guardian-window-overlap"
	analysisBasisParentLogWindow       = "parent-log-window"
)

const analysisWindowEndBasisArchivedAt = "archived-at"

const analysisWindowEndBasisBundleTime = "bundle-time"

const analysisExecutionEndBasisLifecycleComplete = "lifecycle-complete"

const analysisExecutionEndBasisLifecycleInterrupted = "lifecycle-interrupted"

const analysisRetryAfterFail = "previous-task-validation-fail"

const analysisRetryUnknown = "unknown"

const analysisAmbiguityTargetConflicted = "target_call_id_conflicted"

const analysisAmbiguitySourceConflicted = "source_call_id_conflicted"

const analysisWaitYieldClassShort = "short"

const analysisWaitYieldClassBounded = "bounded"

const analysisWaitYieldClassLong = "long"

const codexRolloutTaskStartedType = "task_started"

const codexRolloutTaskCompleteType = "task_complete"

const codexRolloutWaitCallName = "wait"

const codexRolloutFunctionCallType = "function_call"

const codexRolloutFunctionCallOutputType = "function_call_output"

const analysisWaitShortBoundMS = 60000

const analysisWaitLongBoundMS = 21600000

const codexRolloutTokenCountType = "token_count"

func analysisCollectionWindow(task bundleTask) (time.Time, time.Time, string) {
	start := task.Stats.StartedAt.UTC()
	end := time.Now().UTC()
	endBasis := analysisWindowEndBasisBundleTime
	if task.Stats.ArchivedAt != nil && task.Stats.ArchivedAt.Before(end) {
		end = task.Stats.ArchivedAt.UTC()
		endBasis = analysisWindowEndBasisArchivedAt
	}
	return start, end, endBasis
}

func analysisTimestamp(value time.Time) *string {
	encoded := value.UTC().Format(time.RFC3339Nano)
	return &encoded
}

func (c *bundleCollector) addBundleAnalysisIndex(st *state.StateStore, task bundleTask, association codexAssociation) {
	encoded, err := json.MarshalIndent(buildBundleAnalysisIndex(st, task, c, association), "", "  ")
	if err != nil {
		return
	}
	c.addData(bundleAnalysisEntryPath, append(encoded, '\n'))
}

func buildBundleAnalysisIndex(st *state.StateStore, task bundleTask, collector *bundleCollector, association codexAssociation) bundleAnalysisIndex {
	start, collectionEnd, collectionEndBasis := analysisCollectionWindow(task)
	execution := resolveAnalysisExecutionBoundary(st, task.ID)
	rolloutScan := scanAnalysisRolloutWindow(collector, association, start, collectionEnd)
	ownership := resolveAnalysisTaskOwnership(association, rolloutScan.turns, start, collectionEnd, task.ID)
	finalizationInterval := analysisTaskFinalizationInterval(execution, ownership)
	eventRuns := analysisTaskEventValidationRuns(st, task.ID)
	roundSeqByDigest := analysisRoundDigestSeqs(st, task.ID)
	telemetry := scanAnalysisTelemetryCalls(st, task.ID)
	validations, attributedRuns := collectAnalysisValidationRuns(collector, eventRuns, roundSeqByDigest, start, collectionEnd)
	return bundleAnalysisIndex{
		Version:     bundleAnalysisIndexVersion,
		TaskID:      task.ID,
		TaskStatus:  task.Status,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Intervals: bundleAnalysisIntervals{
			TaskExecution:      analysisExecutionInterval(start, execution),
			ParentFinalization: finalizationInterval,
			SubsequentRequests: analysisTaskSubsequentRequests(association, rolloutScan, ownership, collectionEnd),
			Collection: bundleAnalysisInterval{
				Status:   analysisStatusAvailable,
				Start:    analysisTimestamp(start),
				End:      analysisTimestamp(collectionEnd),
				EndBasis: collectionEndBasis,
			},
		},
		ParentSession:  analysisParentSession(association),
		RolloutWindow:  analysisRolloutWindow(association, rolloutScan, start),
		WaitCalls:      analysisWaitCalls(association, rolloutScan, start, execution, collectionEnd),
		TokenDelta:     analysisExecutionTokenDelta(association, rolloutScan, start, execution, collectionEnd),
		Finalization:   analysisTaskFinalizationTokenDelta(association, rolloutScan, execution, ownership, finalizationInterval),
		ValidationRuns: validations,
		Retries:        analysisRetries(task, eventRuns, validations.Runs, telemetry),
		Evidence:       analysisEvidence(collector, association, attributedRuns, validations.Runs),
	}
}

func analysisParentSession(association codexAssociation) bundleAnalysisParent {
	parent := bundleAnalysisParent{
		Status: association.ParentStatus,
		Detail: association.Detail,
	}
	if association.ParentStatus != codexStatusIncluded {
		return parent
	}
	parent.ThreadID = association.ParentThreadID
	parent.AssociationBasis = association.Basis
	parent.RolloutArchivePath = codexRolloutArchivePath(association.ParentThreadID)
	return parent
}

func scanAnalysisRolloutWindow(collector *bundleCollector, association codexAssociation, start, end time.Time) bundleRolloutScan {
	if association.ParentStatus != codexStatusIncluded {
		return bundleRolloutScan{}
	}
	if _, collected := collector.entries[codexRolloutArchivePath(association.ParentThreadID)]; !collected {
		return bundleRolloutScan{}
	}
	scan, err := scanCodexRolloutWindow(association.ParentPath, start, end)
	if err != nil {
		return bundleRolloutScan{}
	}
	return scan
}

func scanCodexRolloutWindow(rolloutPath string, start, end time.Time) (bundleRolloutScan, error) {
	file, err := os.Open(rolloutPath)
	if err != nil {
		return bundleRolloutScan{}, fmt.Errorf("parent rolloutを開けません: %w", err)
	}
	defer func() { _ = file.Close() }()

	scan := bundleRolloutScan{turnIndex: map[string]int{}}
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNumber++
			observeAnalysisRolloutLine(&scan, line, lineNumber, start, end)
			scan.totalBytes += int64(len(line))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				scan.finalizeTurns()
				return scan, nil
			}
			return scan, fmt.Errorf("parent rolloutを読めません: %w", readErr)
		}
	}
}

func (scan *bundleRolloutScan) finalizeTurns() {
	turns := make([]analysisRolloutTurn, 0, len(scan.turns))
	for _, turn := range scan.turns {
		if turn.HasStart {
			turns = append(turns, turn)
		}
	}
	sort.Slice(turns, func(i, j int) bool {
		if turns[i].StartedAt.Equal(turns[j].StartedAt) {
			return turns[i].StartOffset < turns[j].StartOffset
		}
		return turns[i].StartedAt.Before(turns[j].StartedAt)
	})
	scan.turns = turns
	scan.turnIndex = nil
}

func observeAnalysisRolloutLine(scan *bundleRolloutScan, line []byte, lineNumber int, start, end time.Time) {
	trimmed := strings.TrimRight(string(line), "\n")
	if trimmed == "" {
		return
	}
	var record codexRolloutScanLine
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return
	}
	timestamp, timestampErr := time.Parse(time.RFC3339Nano, record.Timestamp)
	if timestampErr != nil {
		return
	}
	observeAnalysisRolloutInWindowRecord(scan, line, timestamp, start, end)
	if record.Type == "response_item" {
		observeAnalysisRolloutWait(scan, record.Payload, timestamp, lineNumber)
	}
	if record.Type == "event_msg" {
		observeAnalysisRolloutEvent(scan, record, timestamp)
	}
}

func observeAnalysisRolloutInWindowRecord(scan *bundleRolloutScan, line []byte, timestamp time.Time, start, end time.Time) {
	if timestamp.Before(start) || timestamp.After(end) {
		return
	}
	if !scan.hasWindow {
		scan.windowStart = scan.totalBytes
		scan.hasWindow = true
	}
	scan.windowEnd = scan.totalBytes + int64(len(line))
}

func observeAnalysisRolloutWait(scan *bundleRolloutScan, payload json.RawMessage, timestamp time.Time, lineNumber int) {
	var item codexRolloutItemPayload
	if err := json.Unmarshal(payload, &item); err != nil {
		return
	}
	switch item.Type {
	case codexRolloutFunctionCallType:
		if item.Name != codexRolloutWaitCallName {
			return
		}
		scan.waits = append(scan.waits, analysisRolloutWaitRequest{
			CallID:  item.CallID,
			Line:    lineNumber,
			At:      timestamp,
			YieldMS: analysisWaitRequestedYield(item.Arguments),
		})
	case codexRolloutFunctionCallOutputType:
		if item.CallID == "" {
			return
		}
		scan.waitReturns = append(scan.waitReturns, analysisRolloutWaitReturn{CallID: item.CallID, Line: lineNumber})
	}
}

func analysisWaitRequestedYield(arguments string) *float64 {
	if arguments == "" {
		return nil
	}
	var parsed struct {
		YieldTimeMS *float64 `json:"yield_time_ms"`
	}
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return nil
	}
	if parsed.YieldTimeMS == nil || *parsed.YieldTimeMS < 0 {
		return nil
	}
	return parsed.YieldTimeMS
}

func observeAnalysisRolloutEvent(scan *bundleRolloutScan, record codexRolloutScanLine, timestamp time.Time) {
	var payload codexRolloutEventPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return
	}
	if payload.Type == codexRolloutTokenCountType {
		observeAnalysisRolloutTokenAnchor(scan, payload, record, timestamp)
		return
	}
	observeAnalysisRolloutTurnBoundary(scan, payload, timestamp)
}

func observeAnalysisRolloutTokenAnchor(scan *bundleRolloutScan, payload codexRolloutEventPayload, record codexRolloutScanLine, timestamp time.Time) {
	usage := payload.Info
	if usage == nil || usage.TotalTokenUsage == nil {
		return
	}
	scan.tokens = append(scan.tokens, analysisRolloutTokenAnchor{
		At:     timestamp,
		RawAt:  record.Timestamp,
		Offset: scan.totalBytes,
		Input:  usage.TotalTokenUsage.InputTokens,
		Cached: usage.TotalTokenUsage.CachedInputTokens,
	})
}

func observeAnalysisRolloutTurnBoundary(scan *bundleRolloutScan, payload codexRolloutEventPayload, timestamp time.Time) {
	if payload.TurnID == "" {
		return
	}
	index, known := scan.turnIndex[payload.TurnID]
	if !known {
		scan.turns = append(scan.turns, analysisRolloutTurn{TurnID: payload.TurnID})
		index = len(scan.turns) - 1
		scan.turnIndex[payload.TurnID] = index
	}
	turn := &scan.turns[index]
	if payload.Type == codexRolloutTaskStartedType && !turn.HasStart {
		turn.StartedAt = timestamp
		turn.StartOffset = scan.totalBytes
		turn.HasStart = true
	}
	if payload.Type == codexRolloutTaskCompleteType && !turn.HasComplete {
		turn.CompletedAt = timestamp
		turn.HasComplete = true
	}
}

func analysisRolloutWindow(association codexAssociation, scan bundleRolloutScan, start time.Time) bundleAnalysisRollout {
	rollout := bundleAnalysisRollout{Status: association.ParentStatus}
	if association.ParentStatus != codexStatusIncluded || !scan.hasWindow {
		return rollout
	}
	rollout.TotalBytes = scan.totalBytes
	rollout.WindowStartOffset = scan.windowStart
	rollout.WindowEndOffset = scan.windowEnd
	rollout.WindowBytes = scan.windowEnd - scan.windowStart
	if baseline, found := lastTokenAnchorAtOrBefore(scan, start); found {
		rollout.BaselineOffset = baseline.Offset
	}
	return rollout
}

func lastTokenAnchorAtOrBefore(scan bundleRolloutScan, bound time.Time) (analysisRolloutTokenAnchor, bool) {
	var found analysisRolloutTokenAnchor
	observed := false
	for _, anchor := range scan.tokens {
		if anchor.At.After(bound) {
			break
		}
		found = anchor
		observed = true
	}
	return found, observed
}

func resolveAnalysisExecutionBoundary(st *state.StateStore, taskID string) analysisExecutionBoundary {
	records, err := state.ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	if err != nil || len(records) == 0 {
		return analysisExecutionBoundary{status: analysisStatusUnknown}
	}
	last := records[len(records)-1]
	switch last.To {
	case string(state.TaskStatusComplete):
		return analysisExecutionBoundary{status: analysisStatusAvailable, end: last.Timestamp, endBasis: analysisExecutionEndBasisLifecycleComplete}
	case string(state.TaskStatusInterrupted):
		return analysisExecutionBoundary{status: analysisStatusAvailable, end: last.Timestamp, endBasis: analysisExecutionEndBasisLifecycleInterrupted}
	default:
		return analysisExecutionBoundary{status: analysisStatusOpen}
	}
}

func resolveAnalysisOwningTurn(turns []analysisRolloutTurn, taskStart time.Time) analysisOwningTurn {
	var containing []*analysisRolloutTurn
	for i := range turns {
		turn := &turns[i]
		if turn.StartedAt.After(taskStart) {
			continue
		}
		if turn.HasComplete && turn.CompletedAt.Before(taskStart) {
			continue
		}
		containing = append(containing, turn)
	}
	if len(containing) != 1 {
		return analysisOwningTurn{status: analysisStatusUnknown}
	}
	return analysisOwningTurn{status: analysisStatusAvailable, turn: containing[0]}
}

func analysisExecutionInterval(start time.Time, execution analysisExecutionBoundary) bundleAnalysisInterval {
	interval := bundleAnalysisInterval{
		Status: execution.status,
		Start:  analysisTimestamp(start),
	}
	if execution.status == analysisStatusAvailable {
		interval.End = analysisTimestamp(execution.end)
		interval.EndBasis = execution.endBasis
	}
	return interval
}

func analysisFinalizationInterval(execution analysisExecutionBoundary, owning analysisOwningTurn) bundleAnalysisInterval {
	interval := bundleAnalysisInterval{Status: analysisStatusUnknown}
	if owning.status != analysisStatusAvailable {
		return interval
	}
	if execution.status == analysisStatusUnknown {
		return interval
	}
	if execution.status == analysisStatusOpen {
		interval.Status = analysisStatusOpen
		return interval
	}
	if !owning.turn.HasComplete {
		interval.Status = analysisStatusOpen
		return interval
	}
	if owning.turn.CompletedAt.Before(execution.end) {
		return interval
	}
	interval.Status = analysisStatusAvailable
	interval.Start = analysisTimestamp(execution.end)
	interval.End = analysisTimestamp(owning.turn.CompletedAt)
	return interval
}

func analysisSubsequentRequests(association codexAssociation, scan bundleRolloutScan, owning analysisOwningTurn, collectionEnd time.Time) bundleAnalysisSubsequents {
	subsequent := bundleAnalysisSubsequents{
		Status:      analysisStatusUnknown,
		Attribution: analysisAttributionSubsequent,
	}
	if association.ParentStatus != codexStatusIncluded || owning.status != analysisStatusAvailable {
		return subsequent
	}
	if collectionEnd.IsZero() {
		return subsequent
	}
	if !owning.turn.HasComplete {
		subsequent.Status = analysisStatusOpen
		return subsequent
	}
	subsequent.Status = analysisStatusAvailable
	for i := range scan.turns {
		turn := &scan.turns[i]
		if !turn.StartedAt.After(owning.turn.CompletedAt) {
			continue
		}
		if turn.StartedAt.After(collectionEnd) {
			continue
		}
		subsequent.Turns = append(subsequent.Turns, analysisSubsequentTurn(scan, turn, collectionEnd))
	}
	return subsequent
}

func analysisSubsequentTurn(scan bundleRolloutScan, turn *analysisRolloutTurn, collectionEnd time.Time) bundleAnalysisSubsequentTurn {
	entry := bundleAnalysisSubsequentTurn{
		TurnID:    turn.TurnID,
		Status:    analysisStatusOpen,
		StartedAt: turn.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	if !turn.HasComplete || turn.CompletedAt.After(collectionEnd) {
		return entry
	}
	entry.Status = analysisStatusAvailable
	completed := turn.CompletedAt.UTC().Format(time.RFC3339Nano)
	entry.CompletedAt = &completed
	delta := analysisAnchoredTokenDelta(scan, turn.StartedAt, turn.CompletedAt)
	if delta.Status != analysisStatusAvailable {
		return entry
	}
	entry.InputTokens = delta.InputTokens
	entry.CachedInputTokens = delta.CachedInputTokens
	entry.BaselineAt = delta.BaselineAt
	entry.EndAt = delta.EndAt
	return entry
}

func analysisWaitCalls(association codexAssociation, scan bundleRolloutScan, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisWaitCalls {
	waits := bundleAnalysisWaitCalls{Status: analysisStatusCounted}
	if association.ParentStatus != codexStatusIncluded {
		waits.Status = association.ParentStatus
		return waits
	}
	if execution.status == analysisStatusUnknown {
		waits.Status = analysisStatusUnknown
		return waits
	}
	if !scan.hasWindow {
		waits.Status = analysisStatusNoObservation
		return waits
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	waits.Calls, waits.DuplicateCallIDs = analysisWaitCallEntries(scan, start, endBound)
	waits.Count = len(waits.Calls)
	if execution.status == analysisStatusOpen {
		waits.Status = analysisStatusOpen
	}
	return waits
}

func analysisWaitCallEntries(scan bundleRolloutScan, start, end time.Time) ([]bundleAnalysisWaitCall, []bundleAnalysisWaitDuplicate) {
	returnLines := analysisWaitReturnLines(scan.waitReturns)
	requestsByCall, order, anonymous := analysisGroupWaitRequests(scan.waits, start, end)
	calls := make([]bundleAnalysisWaitCall, 0, len(order)+len(anonymous))
	duplicates := make([]bundleAnalysisWaitDuplicate, 0)
	for _, callID := range order {
		entry, conflict := analysisWaitCallRecord(callID, requestsByCall[callID])
		if conflict || len(returnLines[callID]) > 1 {
			duplicates = append(duplicates, bundleAnalysisWaitDuplicate{
				CallID:       callID,
				RequestLines: entry.RequestLines,
				ReturnLines:  returnLines[callID],
			})
			continue
		}
		entry.ReturnLines = returnLines[callID]
		calls = append(calls, entry)
	}
	for _, request := range anonymous {
		calls = append(calls, bundleAnalysisWaitCall{
			RequestedYieldMS: request.YieldMS,
			YieldClass:       analysisWaitYieldClass(request.YieldMS),
			RequestLines:     []int{request.Line},
		})
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].RequestLines[0] < calls[j].RequestLines[0]
	})
	return calls, duplicates
}

func analysisWaitReturnLines(waitReturns []analysisRolloutWaitReturn) map[string][]int {
	returnLines := map[string][]int{}
	for _, waitReturn := range waitReturns {
		returnLines[waitReturn.CallID] = append(returnLines[waitReturn.CallID], waitReturn.Line)
	}
	return returnLines
}

func analysisGroupWaitRequests(requests []analysisRolloutWaitRequest, start, end time.Time) (map[string][]analysisRolloutWaitRequest, []string, []analysisRolloutWaitRequest) {
	requestsByCall := map[string][]analysisRolloutWaitRequest{}
	order := make([]string, 0)
	anonymous := make([]analysisRolloutWaitRequest, 0)
	for _, request := range requests {
		if request.At.Before(start) || request.At.After(end) {
			continue
		}
		if request.CallID == "" {
			anonymous = append(anonymous, request)
			continue
		}
		if _, known := requestsByCall[request.CallID]; !known {
			order = append(order, request.CallID)
		}
		requestsByCall[request.CallID] = append(requestsByCall[request.CallID], request)
	}
	return requestsByCall, order, anonymous
}

func analysisWaitCallRecord(callID string, requests []analysisRolloutWaitRequest) (bundleAnalysisWaitCall, bool) {
	entry := bundleAnalysisWaitCall{
		CallID:           callID,
		RequestedYieldMS: requests[0].YieldMS,
		YieldClass:       analysisWaitYieldClass(requests[0].YieldMS),
		RequestLines:     []int{requests[0].Line},
	}
	conflict := false
	for _, request := range requests[1:] {
		entry.RequestLines = append(entry.RequestLines, request.Line)
		if !analysisWaitYieldEqual(requests[0].YieldMS, request.YieldMS) {
			conflict = true
		}
	}
	return entry, conflict
}

func analysisWaitYieldClass(yieldMS *float64) string {
	switch {
	case yieldMS == nil:
		return analysisStatusUnknown
	case *yieldMS < analysisWaitShortBoundMS:
		return analysisWaitYieldClassShort
	case *yieldMS < analysisWaitLongBoundMS:
		return analysisWaitYieldClassBounded
	default:
		return analysisWaitYieldClassLong
	}
}

func analysisWaitYieldEqual(left, right *float64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func analysisExecutionTokenDelta(association codexAssociation, scan bundleRolloutScan, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable}
	if association.ParentStatus != codexStatusIncluded {
		delta.Status = association.ParentStatus
		return delta
	}
	if execution.status == analysisStatusUnknown {
		delta.Status = analysisStatusUnknown
		return delta
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	}
	delta = analysisAnchoredTokenDelta(scan, start, endBound)
	if execution.status == analysisStatusOpen && delta.Status == analysisStatusAvailable {
		delta.Status = analysisStatusOpen
	}
	return delta
}

func analysisFinalizationTokenDelta(association codexAssociation, scan bundleRolloutScan, execution analysisExecutionBoundary, owning analysisOwningTurn, interval bundleAnalysisInterval) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: interval.Status}
	if association.ParentStatus != codexStatusIncluded || interval.Status != analysisStatusAvailable {
		return delta
	}
	return analysisAnchoredTokenDelta(scan, execution.end, owning.turn.CompletedAt)
}

func analysisAnchoredTokenDelta(scan bundleRolloutScan, baselineBound, endBound time.Time) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable}
	baseline, hasBaseline := lastTokenAnchorAtOrBefore(scan, baselineBound)
	end, hasEnd := lastTokenAnchorAtOrBefore(scan, endBound)
	switch {
	case !hasBaseline || !hasEnd:
		delta.Status = analysisStatusMissing
	case end.Offset <= baseline.Offset:
		delta.Status = analysisStatusNoObservation
		delta.BaselineAt = baseline.RawAt
	case end.Input < baseline.Input || end.Cached < baseline.Cached:
		delta.Status = analysisStatusCounterReset
		delta.BaselineAt = baseline.RawAt
		delta.EndAt = end.RawAt
	default:
		delta.InputTokens = end.Input - baseline.Input
		delta.CachedInputTokens = end.Cached - baseline.Cached
		delta.BaselineAt = baseline.RawAt
		delta.EndAt = end.RawAt
	}
	return delta
}

func collectAnalysisValidationRuns(collector *bundleCollector, eventRuns map[string]analysisValidationEvent, roundSeqByDigest map[string]int, start, end time.Time) (bundleAnalysisValidations, map[string]struct{}) {
	validations := bundleAnalysisValidations{Status: analysisStatusNotCollected}
	attributed := map[string]struct{}{}
	runEntries := analysisRunArchiveEntries(collector)
	if len(runEntries) == 0 {
		return validations, attributed
	}

	validations.Status = analysisStatusAvailable
	runs := make([]bundleAnalysisRun, 0, len(runEntries))
	for runID, entry := range runEntries {
		run := analysisRunRecord(runID, entry, eventRuns, roundSeqByDigest, start, end)
		if run.Attribution == analysisAttributionTask {
			attributed[runID] = struct{}{}
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt == runs[j].StartedAt {
			return runs[i].RunID < runs[j].RunID
		}
		return runs[i].StartedAt < runs[j].StartedAt
	})
	validations.Runs = runs
	return validations, attributed
}

func analysisRunArchiveEntries(collector *bundleCollector) map[string]bundleEntry {
	runEntries := map[string]bundleEntry{}
	for archivePath, entry := range collector.entries {
		if !strings.HasPrefix(archivePath, bundleAnalysisRunsArchivePrefix) {
			continue
		}
		if !strings.HasSuffix(archivePath, "/"+qualityGateRunFile) {
			continue
		}
		relative := strings.TrimSuffix(strings.TrimPrefix(archivePath, bundleAnalysisRunsArchivePrefix), "/"+qualityGateRunFile)
		if validValidationRunID(relative) {
			runEntries[relative] = entry
		}
	}
	return runEntries
}

func analysisTaskEventValidationRuns(st *state.StateStore, taskID string) map[string]analysisValidationEvent {
	events := map[string]analysisValidationEvent{}
	recordTaskEventValidationRuns(st.TaskEventLogPath(taskID), events)
	return events
}

func recordTaskEventValidationRuns(eventPath string, events map[string]analysisValidationEvent) {
	file, err := os.Open(eventPath)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil || record.Kind != "validation" || record.Validation == nil {
			continue
		}
		runID := validationEventRunID(record.Validation.Evidence)
		if runID == "" {
			continue
		}
		event := analysisValidationEvent{
			RunID:  runID,
			Form:   record.Validation.Form,
			Result: record.Validation.Result,
			At:     record.Timestamp,
		}
		if existing, duplicated := events[runID]; !duplicated || event.At.Before(existing.At) {
			events[runID] = event
		}
	}
}

func validationEventRunID(evidence string) string {
	if evidence == "" {
		return ""
	}
	relative := strings.TrimPrefix(filepath.ToSlash(evidence), qualityGateRunDirectory+"/")
	runID, _, found := strings.Cut(relative, "/")
	if !found || !validValidationRunID(runID) {
		return ""
	}
	return runID
}

func analysisRoundDigestSeqs(st *state.StateStore, taskID string) map[string]int {
	records, err := st.ReadRoundRecords(taskID)
	if err != nil {
		return nil
	}
	digests := map[string]int{}
	for _, record := range records {
		key := roundSnapshotDigestKey(record.Snapshot.Head, record.Snapshot.IndexDigest, record.Snapshot.WorktreeDigest)
		digests[key] = record.Seq
	}
	return digests
}

func roundSnapshotDigestKey(head, indexDigest, worktreeDigest string) string {
	return strings.Join([]string{head, indexDigest, worktreeDigest}, "\x00")
}

func analysisRunRecord(runID string, entry bundleEntry, eventRuns map[string]analysisValidationEvent, roundSeqByDigest map[string]int, start, end time.Time) bundleAnalysisRun {
	run := bundleAnalysisRun{
		RunID:       runID,
		ArchivePath: entry.ArchivePath,
		Attribution: analysisAttributionUnknown,
		Bases:       []string{},
	}
	record, err := readAnalysisRunRecord(entry.SourcePath)
	if err != nil {
		run.Bases = append(run.Bases, analysisStatusUnreadable)
		return run
	}
	run.Form = record.Form
	run.Result = record.Status
	run.WorkingDir = record.WorkingDir
	run.StartedAt = record.StartedAt.UTC().Format(time.RFC3339Nano)
	if record.CompletedAt != nil {
		run.CompletedAt = record.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if _, linked := eventRuns[runID]; linked {
		run.Attribution = analysisAttributionTask
		run.Bases = append(run.Bases, analysisBasisTaskEventValidation)
		return run
	}
	roundSeq, digestMatched := roundSeqByDigest[roundSnapshotDigestKey(record.Head, record.IndexDigest, record.WorktreeDigest)]
	if digestMatched {
		run.RoundSeq = roundSeq
	}
	overlaps := analysisRunOverlapsWindow(record, start, end)
	switch {
	case digestMatched && overlaps:
		run.Attribution = analysisAttributionTask
		run.Bases = append(run.Bases, analysisBasisRoundSnapshotDigest, analysisBasisWindowOverlap)
	case overlaps:
		run.Attribution = analysisAttributionWindowUnmatched
		run.Bases = append(run.Bases, analysisBasisWindowOverlap)
	case digestMatched:
		run.Attribution = analysisAttributionExternal
		run.Bases = append(run.Bases, analysisBasisRoundSnapshotDigest, analysisBasisOutsideWindow)
	default:
		run.Attribution = analysisAttributionExternal
		run.Bases = append(run.Bases, analysisBasisOutsideWindow)
	}
	return run
}

func analysisRunOverlapsWindow(record qualityGateRunRecord, start, end time.Time) bool {
	if record.StartedAt.IsZero() || record.StartedAt.After(end) {
		return false
	}
	if record.CompletedAt == nil {
		return !record.StartedAt.Before(start)
	}
	return !record.CompletedAt.Before(start)
}

func readAnalysisRunRecord(sourcePath string) (qualityGateRunRecord, error) {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return qualityGateRunRecord{}, err
	}
	var record qualityGateRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return qualityGateRunRecord{}, err
	}
	return record, nil
}

func analysisRetries(task bundleTask, eventRuns map[string]analysisValidationEvent, runs []bundleAnalysisRun, telemetry analysisTelemetryScan) bundleAnalysisRetries {
	retries := bundleAnalysisRetries{
		WorkerCounters:     analysisWorkerRetryCounters(task.Stats),
		ResumedModelCalls:  analysisResumedModelCalls(telemetry),
		ModelCallRelations: analysisModelCallRelations(telemetry, task.ID),
	}
	byRunID := make(map[string]bundleAnalysisRun, len(runs))
	for _, run := range runs {
		byRunID[run.RunID] = run
	}
	retries.ValidationReruns = analysisValidationReruns(eventRuns, byRunID)
	return retries
}

func analysisWorkerRetryCounters(stats state.TaskStats) map[string]int {
	counters := map[string]int{}
	for name, value := range map[string]int{
		"rate_limits":          stats.RateLimits,
		"provider_unavailable": stats.ProviderUnavailable,
		"packet_compactions":   stats.PacketCompactions,
		"resume_commands":      stats.ResumeCommands,
		"auto_fix_rounds":      stats.AutoFixRounds,
		"transient_retries":    stats.TransientRetries,
	} {
		if value > 0 {
			counters[name] = value
		}
	}
	if len(counters) == 0 {
		return nil
	}
	return counters
}

func analysisResumedModelCalls(telemetry analysisTelemetryScan) bundleAnalysisCount {
	count := bundleAnalysisCount{Status: telemetry.status}
	if telemetry.status != analysisStatusAvailable {
		return count
	}
	for _, call := range telemetry.calls {
		if !call.conflicted() {
			if call.Variants[0].Resumed {
				count.Count++
			}
			continue
		}
		resumed, consistent := analysisConflictedCallResumed(call)
		if !consistent {
			count.Status = analysisStatusUnknown
			count.Count = 0
			return count
		}
		if resumed {
			count.Count++
		}
	}
	return count
}

func analysisConflictedCallResumed(call analysisTelemetryCall) (bool, bool) {
	resumed := call.Variants[0].Resumed
	for _, variant := range call.Variants[1:] {
		if variant.Resumed != resumed {
			return false, false
		}
	}
	return resumed, true
}

func analysisValidationReruns(eventRuns map[string]analysisValidationEvent, runs map[string]bundleAnalysisRun) []bundleAnalysisRerun {
	ordered := make([]analysisValidationEvent, 0, len(eventRuns))
	for _, event := range eventRuns {
		ordered = append(ordered, event)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].At.Equal(ordered[j].At) {
			return ordered[i].RunID < ordered[j].RunID
		}
		return ordered[i].At.Before(ordered[j].At)
	})

	reruns := make([]bundleAnalysisRerun, 0)
	previousByForm := map[string]analysisValidationEvent{}
	for _, event := range ordered {
		previous, existed := previousByForm[event.Form]
		previousByForm[event.Form] = event
		if !existed {
			continue
		}
		rerun := bundleAnalysisRerun{
			RunID:         event.RunID,
			Form:          event.Form,
			Reason:        analysisRetryUnknown,
			PreviousRunID: previous.RunID,
		}
		if previous.Result == state.ValidationResultFail {
			if _, collected := runs[event.RunID]; collected {
				rerun.Reason = analysisRetryAfterFail
			}
		}
		reruns = append(reruns, rerun)
	}
	if len(reruns) == 0 {
		return nil
	}
	return reruns
}

func analysisEvidence(collector *bundleCollector, association codexAssociation, attributedRuns map[string]struct{}, runs []bundleAnalysisRun) bundleAnalysisEvidence {
	external := map[string]struct{}{}
	explained := make(map[string]struct{}, len(attributedRuns)+len(runs))
	for runID := range attributedRuns {
		explained[runID] = struct{}{}
	}
	for _, run := range runs {
		if run.Attribution != analysisAttributionExternal {
			continue
		}
		external[run.RunID] = struct{}{}
		explained[run.RunID] = struct{}{}
	}
	return bundleAnalysisEvidence{
		Task:          analysisTaskEvidence(collector),
		ParentSession: analysisParentEvidence(collector, association),
		Unattributed:  analysisUnattributedEvidence(collector, explained),
		TaskExternal:  analysisExternalRunEvidence(external),
	}
}

func analysisTaskEvidence(collector *bundleCollector) []bundleAnalysisEvidenceRef {
	refs := []bundleAnalysisEvidenceRef{{ArchivePath: "task/", Basis: analysisBasisTaskScopedState}}
	transcripts := make([]string, 0)
	for archivePath := range collector.entries {
		if strings.HasPrefix(archivePath, "claude-transcripts/") {
			transcripts = append(transcripts, archivePath)
		}
	}
	sort.Strings(transcripts)
	for _, transcript := range transcripts {
		refs = append(refs, bundleAnalysisEvidenceRef{ArchivePath: transcript, Basis: analysisBasisModelCallSession})
	}
	return refs
}

func analysisParentEvidence(collector *bundleCollector, association codexAssociation) []bundleAnalysisEvidenceRef {
	if association.ParentStatus != codexStatusIncluded {
		return nil
	}
	refs := []bundleAnalysisEvidenceRef{{
		ArchivePath: codexRolloutArchivePath(association.ParentThreadID),
		Basis:       association.Basis + ";" + analysisBasisParentRolloutWindow,
	}}
	for _, guardian := range association.Guardians {
		refs = append(refs, bundleAnalysisEvidenceRef{
			ArchivePath: codexGuardianArchivePath(guardian.ID),
			Basis:       association.Basis + ";" + analysisBasisGuardianWindowOverlap,
		})
	}
	logPaths := make([]string, 0)
	for archivePath := range collector.entries {
		if strings.HasPrefix(archivePath, "codex-parent/logs/") {
			logPaths = append(logPaths, archivePath)
		}
	}
	sort.Strings(logPaths)
	for _, logPath := range logPaths {
		refs = append(refs, bundleAnalysisEvidenceRef{
			ArchivePath: logPath,
			Basis:       association.Basis + ";" + analysisBasisParentLogWindow,
		})
	}
	return refs
}

func analysisUnattributedEvidence(collector *bundleCollector, attributedRuns map[string]struct{}) []bundleAnalysisEvidenceRef {
	refs := make([]bundleAnalysisEvidenceRef, 0)
	for _, archivePath := range collector.unattributedList() {
		if analysisRunAttributed(archivePath, attributedRuns) {
			continue
		}
		refs = append(refs, bundleAnalysisEvidenceRef{ArchivePath: archivePath, Basis: analysisBasisCurrentState})
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

func analysisRunAttributed(archivePath string, attributedRuns map[string]struct{}) bool {
	if !strings.HasPrefix(archivePath, bundleAnalysisRunsArchivePrefix) {
		return false
	}
	remaining := strings.TrimPrefix(archivePath, bundleAnalysisRunsArchivePrefix)
	for runID := range attributedRuns {
		if strings.HasPrefix(remaining, runID+"/") {
			return true
		}
	}
	return false
}

func analysisExternalRunEvidence(externalRuns map[string]struct{}) []bundleAnalysisEvidenceRef {
	refs := make([]bundleAnalysisEvidenceRef, 0, len(externalRuns))
	for runID := range externalRuns {
		refs = append(refs, bundleAnalysisEvidenceRef{
			ArchivePath: path.Join(bundleAnalysisRunsArchivePrefix, runID, qualityGateRunFile),
			Basis:       analysisBasisOutsideWindow,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ArchivePath < refs[j].ArchivePath })
	if len(refs) == 0 {
		return nil
	}
	return refs
}
