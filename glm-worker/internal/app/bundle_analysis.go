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
	WaitCalls      bundleAnalysisCount       `json:"parent_wait_calls"`
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
	Basis    string  `json:"basis"`
}

type bundleAnalysisSubsequents struct {
	Status      string                         `json:"status"`
	Attribution string                         `json:"attribution"`
	Turns       []bundleAnalysisSubsequentTurn `json:"turns,omitempty"`
	Basis       string                         `json:"basis"`
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
	Note              string `json:"note,omitempty"`
}

type bundleAnalysisCount struct {
	Status string `json:"status"`
	Count  int    `json:"count,omitempty"`
	Basis  string `json:"basis"`
}

type bundleAnalysisTokenDelta struct {
	Status            string `json:"status"`
	InputTokens       int64  `json:"input_tokens,omitempty"`
	CachedInputTokens int64  `json:"cached_input_tokens,omitempty"`
	BaselineAt        string `json:"baseline_at,omitempty"`
	EndAt             string `json:"end_at,omitempty"`
	Basis             string `json:"basis"`
}

type bundleAnalysisValidations struct {
	Status string              `json:"status"`
	Runs   []bundleAnalysisRun `json:"runs,omitempty"`
	Rule   string              `json:"attribution_rule"`
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
	ValidationReruns  []bundleAnalysisRerun `json:"validation_reruns,omitempty"`
	WorkerCounters    map[string]int        `json:"worker_counters,omitempty"`
	ResumedModelCalls bundleAnalysisCount   `json:"resumed_model_calls"`
	Basis             string                `json:"basis"`
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
	waits       []time.Time
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
	Type string `json:"type"`
	Name string `json:"name"`
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

const bundleAnalysisIndexVersion = 2

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
	analysisBasisTaskEventValidation = "task-event-validation"
	analysisBasisRoundSnapshotDigest = "round-snapshot-digest"
	analysisBasisWindowOverlap       = "window-overlap"
	analysisBasisOutsideWindow       = "outside-window"
	analysisBasisTaskScopedState     = "task-scoped-state-store"
	analysisBasisModelCallSession    = "model-call-log-session"
	analysisBasisCurrentState        = "bundle-time-current-state"
)

const analysisWindowEndBasisArchivedAt = "archived-at"

const analysisWindowEndBasisBundleTime = "bundle-time"

const analysisExecutionEndBasisLifecycleComplete = "lifecycle-complete"

const analysisExecutionEndBasisLifecycleInterrupted = "lifecycle-interrupted"

const analysisWaitBasis = "count of response_item function_call records with name=wait in the parent rollout whose timestamp falls inside the task_execution interval inclusive of both bounds; waits observed after the terminal lifecycle boundary are excluded"

const analysisTokenBasis = "delta of cumulative total_token_usage between the last token_count record at or before task_execution start and the last token_count record at or before task_execution end in the parent rollout; token observation counts, not billing amounts; cached input is reported separately and is not re-added to input; a record at or before a phase boundary belongs to the earlier phase so re-collection cannot change a terminal task's delta"

const analysisFinalizationTokenBasis = "delta of cumulative total_token_usage between the task_execution end anchor and the last token_count record at or before the owning parent turn task_complete; parent-side token observation counts kept separate from GLM token schemas and never added to task consumption"

const analysisSubsequentTokenBasis = "per turn, delta of cumulative total_token_usage between the last token_count record at or before the turn task_started and the last token_count record at or before the turn task_complete; omitted values mean no anchored observation; never added to task consumption"

const analysisOpenObservationNote = "; task_execution has no terminal lifecycle boundary yet so the observation is bounded by the collection interval and may change until the task reaches a terminal lifecycle state"

const analysisExecutionIntervalBasis = "start is task stats started_at; end is the final task lifecycle transition to complete or interrupted; records at or before the boundary timestamp belong to task_execution"

const analysisFinalizationIntervalBasis = "starts strictly after the task_execution boundary and ends at the task_complete of the unique parent rollout turn containing the task start; the boundary record itself is counted in task_execution so phases never double count; an inverted boundary where the owning turn completes before the task terminal transition leaves this interval unknown"

const analysisSubsequentIntervalBasis = "parent rollout turns whose task_started is after the owning turn task_complete; attribution to any GLM task is undetermined; enumeration is bounded by the collection interval end, so turns starting after that end are not listed and turns completing after it stay open with end null and no post-end anchors"

const analysisCollectionIntervalBasis = "traditional collection range from task started_at to archived-at or bundle generation time; collection growth never changes terminal task_execution values"

const analysisRetryAfterFail = "previous-task-validation-fail"

const analysisRetryUnknown = "unknown"

const analysisRolloutSpansTasksNote = "rollout spans multiple tasks and parent turns; analysis uses the collection interval byte range and anchored counter deltas instead of whole-file totals"

const codexRolloutTaskStartedType = "task_started"

const codexRolloutTaskCompleteType = "task_complete"

const analysisValidationRule = "task-event-validation wins; otherwise round-snapshot-digest within the window attributes the run to the task; window-overlap without identity stays unattributed; runs outside the window are task-external"

const analysisRetryBasis = "validation reruns derive from the task event validation sequence; counters come from task stats; resumed calls come from the model call logs"

const analysisResumedCallsBasis = "model call logs with resumed=true for this task"

const codexRolloutWaitCallName = "wait"

const codexRolloutFunctionCallType = "function_call"

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
	owning := resolveAnalysisOwningTurn(rolloutScan.turns, start)
	finalizationInterval := analysisFinalizationInterval(execution, owning)
	eventRuns := analysisTaskEventValidationRuns(st, task.ID)
	roundSeqByDigest := analysisRoundDigestSeqs(st, task.ID)
	validations, attributedRuns := collectAnalysisValidationRuns(collector, eventRuns, roundSeqByDigest, start, collectionEnd)
	return bundleAnalysisIndex{
		Version:     bundleAnalysisIndexVersion,
		TaskID:      task.ID,
		TaskStatus:  task.Status,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Intervals: bundleAnalysisIntervals{
			TaskExecution:      analysisExecutionInterval(start, execution),
			ParentFinalization: finalizationInterval,
			SubsequentRequests: analysisSubsequentRequests(association, rolloutScan, owning, collectionEnd),
			Collection: bundleAnalysisInterval{
				Status:   analysisStatusAvailable,
				Start:    analysisTimestamp(start),
				End:      analysisTimestamp(collectionEnd),
				EndBasis: collectionEndBasis,
				Basis:    analysisCollectionIntervalBasis,
			},
		},
		ParentSession:  analysisParentSession(association),
		RolloutWindow:  analysisRolloutWindow(association, rolloutScan, start),
		WaitCalls:      analysisWaitCalls(association, rolloutScan, start, execution, collectionEnd),
		TokenDelta:     analysisExecutionTokenDelta(association, rolloutScan, start, execution, collectionEnd),
		Finalization:   analysisFinalizationTokenDelta(association, rolloutScan, execution, owning, finalizationInterval),
		ValidationRuns: validations,
		Retries:        analysisRetries(st, task, eventRuns, validations.Runs),
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
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			observeAnalysisRolloutLine(&scan, line, start, end)
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

func observeAnalysisRolloutLine(scan *bundleRolloutScan, line []byte, start, end time.Time) {
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
		observeAnalysisRolloutWait(scan, record.Payload, timestamp)
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

func observeAnalysisRolloutWait(scan *bundleRolloutScan, payload json.RawMessage, timestamp time.Time) {
	var item codexRolloutItemPayload
	if err := json.Unmarshal(payload, &item); err != nil {
		return
	}
	if item.Type == codexRolloutFunctionCallType && item.Name == codexRolloutWaitCallName {
		scan.waits = append(scan.waits, timestamp)
	}
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
	rollout := bundleAnalysisRollout{Status: association.ParentStatus, Note: analysisRolloutSpansTasksNote}
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
		Basis:  analysisExecutionIntervalBasis,
	}
	if execution.status == analysisStatusAvailable {
		interval.End = analysisTimestamp(execution.end)
		interval.EndBasis = execution.endBasis
	}
	return interval
}

func analysisFinalizationInterval(execution analysisExecutionBoundary, owning analysisOwningTurn) bundleAnalysisInterval {
	interval := bundleAnalysisInterval{Status: analysisStatusUnknown, Basis: analysisFinalizationIntervalBasis}
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
		Basis:       analysisSubsequentIntervalBasis + "; " + analysisSubsequentTokenBasis,
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
	delta := analysisAnchoredTokenDelta(scan, turn.StartedAt, turn.CompletedAt, analysisSubsequentTokenBasis)
	if delta.Status != analysisStatusAvailable {
		return entry
	}
	entry.InputTokens = delta.InputTokens
	entry.CachedInputTokens = delta.CachedInputTokens
	entry.BaselineAt = delta.BaselineAt
	entry.EndAt = delta.EndAt
	return entry
}

func analysisWaitCalls(association codexAssociation, scan bundleRolloutScan, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisCount {
	count := bundleAnalysisCount{Status: analysisStatusCounted, Basis: analysisWaitBasis}
	if association.ParentStatus != codexStatusIncluded {
		count.Status = association.ParentStatus
		return count
	}
	if execution.status == analysisStatusUnknown {
		count.Status = analysisStatusUnknown
		return count
	}
	endBound := collectionEnd
	if execution.status == analysisStatusAvailable {
		endBound = execution.end
	} else {
		count.Basis += analysisOpenObservationNote
	}
	if !scan.hasWindow {
		count.Status = analysisStatusNoObservation
		return count
	}
	for _, at := range scan.waits {
		if at.Before(start) || at.After(endBound) {
			continue
		}
		count.Count++
	}
	if execution.status == analysisStatusOpen {
		count.Status = analysisStatusOpen
	}
	return count
}

func analysisExecutionTokenDelta(association codexAssociation, scan bundleRolloutScan, start time.Time, execution analysisExecutionBoundary, collectionEnd time.Time) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable, Basis: analysisTokenBasis}
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
	} else {
		delta.Basis += analysisOpenObservationNote
	}
	delta = analysisAnchoredTokenDelta(scan, start, endBound, delta.Basis)
	if execution.status == analysisStatusOpen && delta.Status == analysisStatusAvailable {
		delta.Status = analysisStatusOpen
	}
	return delta
}

func analysisFinalizationTokenDelta(association codexAssociation, scan bundleRolloutScan, execution analysisExecutionBoundary, owning analysisOwningTurn, interval bundleAnalysisInterval) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: interval.Status, Basis: analysisFinalizationTokenBasis}
	if association.ParentStatus != codexStatusIncluded || interval.Status != analysisStatusAvailable {
		return delta
	}
	return analysisAnchoredTokenDelta(scan, execution.end, owning.turn.CompletedAt, analysisFinalizationTokenBasis)
}

func analysisAnchoredTokenDelta(scan bundleRolloutScan, baselineBound, endBound time.Time, basis string) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: analysisStatusAvailable, Basis: basis}
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
	validations := bundleAnalysisValidations{
		Status: analysisStatusNotCollected,
		Rule:   analysisValidationRule,
	}
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

func analysisRetries(st *state.StateStore, task bundleTask, eventRuns map[string]analysisValidationEvent, runs []bundleAnalysisRun) bundleAnalysisRetries {
	retries := bundleAnalysisRetries{
		WorkerCounters:    analysisWorkerRetryCounters(task.Stats),
		ResumedModelCalls: analysisResumedModelCalls(st, task.ID),
		Basis:             analysisRetryBasis,
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

func analysisResumedModelCalls(st *state.StateStore, taskID string) bundleAnalysisCount {
	count := bundleAnalysisCount{Status: analysisStatusAvailable, Basis: analysisResumedCallsBasis}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		count.Status = analysisStatusMissing
		return count
	}
	resumed := 0
	for _, log := range logs {
		if log.Resumed {
			resumed++
		}
	}
	count.Count = resumed
	return count
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
		Basis:       association.Basis + "; " + analysisRolloutSpansTasksNote,
	}}
	for _, guardian := range association.Guardians {
		refs = append(refs, bundleAnalysisEvidenceRef{
			ArchivePath: codexGuardianArchivePath(guardian.ID),
			Basis:       association.Basis + "; guardian child overlapping the task window",
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
			Basis:       association.Basis + "; extracted bounded by the associated threads and the task window",
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
