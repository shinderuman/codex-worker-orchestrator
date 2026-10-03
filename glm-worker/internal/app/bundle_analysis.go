package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

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
	ThreadID            string   `json:"thread_id,omitempty"`
	Status              string   `json:"status"`
	AssociationBasis    string   `json:"association_basis,omitempty"`
	RolloutArchivePath  string   `json:"rollout_archive_path,omitempty"`
	RolloutArchivePaths []string `json:"rollout_archive_paths,omitempty"`
	RolloutSources      []string `json:"rollout_sources,omitempty"`
	Detail              string   `json:"detail,omitempty"`
}

type bundleAnalysisRollout struct {
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	Source            string `json:"source,omitempty"`
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
	totalBytes     int64
	windowStart    int64
	windowEnd      int64
	hasWindow      bool
	windowRecords  []time.Time
	fileCount      int
	turns          []analysisRolloutTurn
	turnIndex      map[string]int
	tokens         []analysisRolloutTokenAnchor
	waits          []analysisRolloutWaitRequest
	waitReturns    []analysisRolloutWaitReturn
	toolEvents     []analysisRolloutToolEvent
	compactions    []analysisRolloutCompaction
	resumeCommands []analysisRolloutResumeCommand
}

type analysisRolloutResumeCommand struct {
	TurnID  string
	Command string
	Stdout  string
	At      time.Time
}

type analysisRolloutToolEvent struct {
	At          time.Time
	Call        bool
	OutputBytes int64
}

type analysisRolloutCompaction struct {
	At time.Time
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
	At        time.Time
	RawAt     string
	Offset    int64
	Line      int
	File      int
	Source    string
	Input     *int64
	Cached    *int64
	Output    *int64
	Reasoning *int64
	Total     *int64
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
	LastTokenUsage  *codexRolloutTokenUsage `json:"last_token_usage"`
}

type codexRolloutTokenUsage struct {
	InputTokens          *int64 `json:"input_tokens"`
	CachedInputTokens    *int64 `json:"cached_input_tokens"`
	OutputTokens         *int64 `json:"output_tokens"`
	ReasoningOutputToken *int64 `json:"reasoning_output_tokens"`
	TotalTokens          *int64 `json:"total_tokens"`
}

type codexRolloutItemPayload struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	CallID string `json:"call_id"`
	Input  string `json:"input"`
}

type codexRolloutToolPayload struct {
	Type   string          `json:"type"`
	Output json.RawMessage `json:"output"`
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

type analysisCounterDelta struct {
	Value             int64
	Known             bool
	MissingInBaseline bool
	MissingInEnd      bool
}

type analysisTokenSegment struct {
	file     int
	baseline *analysisRolloutTokenAnchor
	end      *analysisRolloutTokenAnchor
}

const bundleAnalysisIndexVersion = 5

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

const analysisReasonRolloutScanFailed = "rollout-scan-failed"

const analysisWindowEndBasisArchivedAt = "archived-at"

const analysisWindowEndBasisBundleTime = "bundle-time"

const analysisExecutionEndBasisLifecycleComplete = "lifecycle-complete"

const analysisExecutionEndBasisLifecycleInterrupted = "lifecycle-interrupted"

const analysisAmbiguityTargetConflicted = "target_call_id_conflicted"

const analysisAmbiguitySourceConflicted = "source_call_id_conflicted"

const analysisWaitYieldClassShort = "short"

const codexRolloutTaskStartedType = "task_started"

const codexRolloutTaskCompleteType = "task_complete"

const codexRolloutFunctionCallType = "function_call"

const codexRolloutFunctionCallOutputType = "function_call_output"

const codexRolloutCustomToolCallType = "custom_tool_call"

const codexRolloutCustomToolCallOutputType = "custom_tool_call_output"

const codexRolloutCompactedType = "compacted"

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

func scanCodexRolloutChainWindow(chain []codexRollout, start, end time.Time) (bundleRolloutScan, error) {
	scan := bundleRolloutScan{turnIndex: map[string]int{}}
	for _, member := range chain {
		if err := scanCodexRolloutChainMember(&scan, member, start, end); err != nil {
			return scan, err
		}
	}
	scan.finalizeTurns()
	return scan, nil
}

func scanCodexRolloutChainMember(scan *bundleRolloutScan, member codexRollout, start, end time.Time) error {
	file, err := os.Open(member.AbsolutePath)
	if err != nil {
		return fmt.Errorf("parent rolloutを開けません: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileIndex := scan.fileCount
	scan.fileCount++
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNumber++
			if err := observeAnalysisRolloutLine(scan, line, lineNumber, fileIndex, member.HomeRelative, start, end); err != nil {
				return err
			}
			scan.totalBytes += int64(len(line))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("parent rolloutを読めません: %w", readErr)
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

func observeAnalysisRolloutLine(scan *bundleRolloutScan, line []byte, lineNumber, fileIndex int, source string, start, end time.Time) error {
	trimmed := strings.TrimRight(string(line), "\n")
	if trimmed == "" {
		return nil
	}
	var record codexRolloutScanLine
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil {
		return fmt.Errorf("parent rollout %d行目のJSONを解析できません: %w", lineNumber, err)
	}
	timestamp, timestampErr := time.Parse(time.RFC3339Nano, record.Timestamp)
	if timestampErr != nil {
		return fmt.Errorf("parent rollout %d行目のtimestampを解析できません: %w", lineNumber, timestampErr)
	}
	observeAnalysisRolloutInWindowRecord(scan, line, timestamp, start, end)
	if record.Type == "response_item" {
		observeAnalysisRolloutWait(scan, record.Payload, timestamp, lineNumber)
		observeAnalysisRolloutToolActivity(scan, record.Payload, timestamp)
	}
	if record.Type == "event_msg" {
		observeAnalysisRolloutEvent(scan, record, timestamp, lineNumber, fileIndex, source)
	}
	if record.Type == codexRolloutCompactedType {
		scan.compactions = append(scan.compactions, analysisRolloutCompaction{At: timestamp})
	}
	return nil
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
	scan.windowRecords = append(scan.windowRecords, timestamp)
}

func observeAnalysisRolloutWait(scan *bundleRolloutScan, payload json.RawMessage, timestamp time.Time, lineNumber int) {
	var item codexRolloutItemPayload
	if err := json.Unmarshal(payload, &item); err != nil {
		return
	}
	switch item.Type {
	case codexRolloutCustomToolCallType:
		if item.Name != "exec" {
			return
		}
		yieldMS, recognized := analysisCustomWaitRequestedYield(item.Input)
		if !recognized {
			return
		}
		scan.waits = append(scan.waits, analysisRolloutWaitRequest{
			CallID:  item.CallID,
			Line:    lineNumber,
			At:      timestamp,
			YieldMS: yieldMS,
		})
	case codexRolloutCustomToolCallOutputType:
		if item.CallID == "" {
			return
		}
		scan.waitReturns = append(scan.waitReturns, analysisRolloutWaitReturn{CallID: item.CallID, Line: lineNumber})
	}
}

func observeAnalysisRolloutToolActivity(scan *bundleRolloutScan, payload json.RawMessage, timestamp time.Time) {
	var item codexRolloutToolPayload
	if err := json.Unmarshal(payload, &item); err != nil {
		return
	}
	switch item.Type {
	case codexRolloutFunctionCallType, codexRolloutCustomToolCallType:
		scan.toolEvents = append(scan.toolEvents, analysisRolloutToolEvent{At: timestamp, Call: true})
	case codexRolloutFunctionCallOutputType, codexRolloutCustomToolCallOutputType:
		scan.toolEvents = append(scan.toolEvents, analysisRolloutToolEvent{
			At:          timestamp,
			OutputBytes: analysisToolOutputBytes(item.Output),
		})
	}
}

func analysisToolOutputBytes(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return int64(len(text))
	}
	var items []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	var total int64
	for _, item := range items {
		total += int64(len(item.Text))
	}
	return total
}

func observeAnalysisRolloutEvent(scan *bundleRolloutScan, record codexRolloutScanLine, timestamp time.Time, lineNumber, fileIndex int, source string) {
	var payload codexRolloutEventPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return
	}
	if payload.Type == codexRolloutTokenCountType {
		observeAnalysisRolloutTokenAnchor(scan, payload, record, timestamp, lineNumber, fileIndex, source)
		return
	}
	observeAnalysisRolloutTurnBoundary(scan, payload, timestamp)
	analysisObserveRolloutResumeCommand(scan, record.Payload, timestamp)
}

func analysisObserveRolloutResumeCommand(scan *bundleRolloutScan, payload json.RawMessage, timestamp time.Time) {
	turnID, command, stdout, ok := analysisResumeCommandFromPayload(payload)
	if !ok {
		return
	}
	scan.resumeCommands = append(scan.resumeCommands, analysisRolloutResumeCommand{
		TurnID:  turnID,
		Command: command,
		Stdout:  stdout,
		At:      timestamp,
	})
}

func observeAnalysisRolloutTokenAnchor(scan *bundleRolloutScan, payload codexRolloutEventPayload, record codexRolloutScanLine, timestamp time.Time, lineNumber, fileIndex int, source string) {
	usage := payload.Info
	if usage == nil || usage.TotalTokenUsage == nil {
		return
	}
	scan.tokens = append(scan.tokens, analysisRolloutTokenAnchor{
		At:        timestamp,
		RawAt:     record.Timestamp,
		Offset:    scan.totalBytes,
		Line:      lineNumber,
		File:      fileIndex,
		Source:    source,
		Input:     usage.TotalTokenUsage.InputTokens,
		Cached:    usage.TotalTokenUsage.CachedInputTokens,
		Output:    usage.TotalTokenUsage.OutputTokens,
		Reasoning: usage.TotalTokenUsage.ReasoningOutputToken,
		Total:     usage.TotalTokenUsage.TotalTokens,
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
