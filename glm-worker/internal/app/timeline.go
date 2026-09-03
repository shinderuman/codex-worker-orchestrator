package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type timelineOutput struct {
	TaskID        string               `json:"task_id"`
	TaskStatus    *string              `json:"task_status"`
	Coverage      timelineCoverage     `json:"coverage"`
	EventLog      timelineEventLog     `json:"event_log"`
	SkippedEvents int                  `json:"skipped_events,omitempty"`
	Telemetry     *string              `json:"telemetry"`
	Calls         []timelineCall       `json:"calls"`
	ToolTotals    []timelineTool       `json:"tool_totals"`
	SessionAging  []state.SessionAging `json:"session_aging"`
}

type timelineCoverage struct {
	Status         string               `json:"status"`
	MissingSources []string             `json:"missing_sources,omitempty"`
	Sources        timelineSourceStates `json:"sources"`
}

type timelineSourceStates struct {
	EventLog  timelineSourceState `json:"event_log"`
	Telemetry timelineSourceState `json:"telemetry"`
	TaskStats timelineSourceState `json:"task_stats"`
}

type timelineSourceState struct {
	Status  string `json:"status"`
	Path    string `json:"path,omitempty"`
	Records int    `json:"records,omitempty"`
}

type timelineEventLog struct {
	Status string  `json:"status"`
	Path   *string `json:"path,omitempty"`
}

type timelineCall struct {
	Index            int                `json:"index"`
	Role             *string            `json:"role"`
	Phase            *string            `json:"phase"`
	CallID           string             `json:"call_id"`
	SessionID        *string            `json:"session_id"`
	SessionCallIndex int                `json:"session_call_index"`
	Resumed          bool               `json:"resumed,omitempty"`
	ModelAlias       *string            `json:"model_alias"`
	MessageModel     *string            `json:"message_model,omitempty"`
	FirstAt          *time.Time         `json:"first_at"`
	LastAt           *time.Time         `json:"last_at"`
	SpanMS           *int64             `json:"span_ms"`
	Events           int                `json:"events"`
	Result           timelineCallResult `json:"result"`
	Tools            []timelineTool     `json:"tools"`
}

type timelineCallResult struct {
	Observed      bool                  `json:"observed"`
	Subtype       *string               `json:"subtype,omitempty"`
	IsError       bool                  `json:"is_error,omitempty"`
	DurationMS    *int64                `json:"duration_ms,omitempty"`
	APIDurationMS *int64                `json:"api_duration_ms,omitempty"`
	Turns         int                   `json:"turns,omitempty"`
	Usage         *state.TaskEventUsage `json:"usage,omitempty"`
	TotalCostUSD  float64               `json:"total_cost_usd,omitempty"`
}

type timelineTool struct {
	Name          string `json:"name"`
	Uses          int    `json:"uses"`
	Results       int    `json:"results,omitempty"`
	Measured      int    `json:"measured,omitempty"`
	MeasuredSumMS int64  `json:"measured_sum_ms,omitempty"`
	MeasuredMaxMS int64  `json:"measured_max_ms,omitempty"`
	Unmeasured    int    `json:"unmeasured,omitempty"`
	Errors        int    `json:"errors,omitempty"`
}

type eventLogSkippedLine struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

const (
	timelineStatusComplete = "complete"
	timelineStatusPartial  = "partial"
	timelineStatusUnknown  = "unknown"

	timelineSourceOK = "ok"
)

func printTimeline(st *state.StateStore, taskIDArg string, stdout io.Writer) error {
	explicit := taskIDArg != ""
	taskID := taskIDArg
	if taskID == "" {
		taskID = st.ReadOr("task.id", "")
	}
	if !validTimelineTaskID(taskID, explicit) {
		return &UsageError{Message: fmt.Sprintf("task IDが生成されるUUID v4形式と一致しません: %q", taskID)}
	}

	records, skipped, eventErr := readTaskEventRecords(st, taskID)
	if eventErr != nil && explicit && !errors.Is(eventErr, os.ErrNotExist) {
		return fmt.Errorf("task %sのevent logを読めません: %w", taskID, eventErr)
	}
	logs, telemetryErr := readTimelineTelemetry(st, taskID)

	output := timelineOutput{
		TaskID:     taskID,
		TaskStatus: timelineTaskStatus(st, taskID, explicit),
	}
	output.EventLog = timelineEventLogState(st, taskID, eventErr)
	if output.EventLog.Status == timelineSourceOK {
		output.Calls = timelineCalls(records)
		output.ToolTotals = timelineTools(state.SumCallTimelineTools(state.CallsFromTaskEvents(records)))
		output.SkippedEvents = skipped
	}
	fillTimelineTelemetry(taskID, telemetryErr, logs, &output)
	statsSource := timelineTaskStatsSource(st, taskID)
	if explicit && errors.Is(eventErr, os.ErrNotExist) && !timelineTaskProven(output.SessionAging, statsSource) {
		return &NotFoundError{Message: fmt.Sprintf("task %sのevent logがありません: %v", taskID, eventErr)}
	}
	output.Coverage = buildTimelineCoverage(st, taskID, output.EventLog.Status, len(records), telemetryErr, len(logs), output.SessionAging, statsSource)
	return writeJSON(stdout, output)
}

func timelineEventLogState(st *state.StateStore, taskID string, err error) timelineEventLog {
	switch {
	case err == nil:
		return timelineEventLog{Status: timelineSourceOK, Path: stringPtr(st.TaskEventLogPath(taskID))}
	case errors.Is(err, os.ErrNotExist):
		return timelineEventLog{Status: statusNone}
	default:
		return timelineEventLog{Status: statusUnreadable}
	}
}

func readTimelineTelemetry(st *state.StateStore, taskID string) ([]state.ModelCallLog, error) {
	if taskID == "" {
		return nil, nil
	}
	return st.ReadModelCallLogs(taskID)
}

func fillTimelineTelemetry(taskID string, logErr error, logs []state.ModelCallLog, output *timelineOutput) {
	if taskID == "" {
		return
	}
	if logErr != nil && !errors.Is(logErr, os.ErrNotExist) {
		unreadable := statusUnreadable
		output.Telemetry = &unreadable
		return
	}
	ok := timelineSourceOK
	output.Telemetry = &ok
	output.SessionAging = state.AgingFromModelCallLogs(logs)
}

func buildTimelineCoverage(st *state.StateStore, taskID string, eventLogStatus string, eventRecords int, telemetryErr error, telemetryRecords int, aging []state.SessionAging, statsSource timelineSourceState) timelineCoverage {
	sources := timelineSourceStates{
		EventLog:  timelineEventLogSource(st, taskID, eventLogStatus, eventRecords),
		Telemetry: timelineTelemetrySource(st, taskID, telemetryErr, telemetryRecords),
		TaskStats: statsSource,
	}
	return timelineCoverage{
		Status:         timelineOverallStatus(eventLogStatus, aging),
		MissingSources: timelineMissingSources(sources),
		Sources:        sources,
	}
}

func timelineOverallStatus(eventLogStatus string, aging []state.SessionAging) string {
	switch {
	case eventLogStatus == timelineSourceOK:
		return timelineStatusComplete
	case len(aging) > 0:
		return timelineStatusPartial
	default:
		return timelineStatusUnknown
	}
}

func timelineMissingSources(sources timelineSourceStates) []string {
	missing := make([]string, 0, 3)
	if sources.EventLog.Status == statusNone {
		missing = append(missing, "event_log")
	}
	if sources.Telemetry.Status == statusNone {
		missing = append(missing, "telemetry")
	}
	if sources.TaskStats.Status == statusNone {
		missing = append(missing, "task_stats")
	}
	return missing
}

func timelineEventLogSource(st *state.StateStore, taskID string, status string, records int) timelineSourceState {
	return timelineSourceState{
		Status:  status,
		Path:    timelineLocator(taskID, st.TaskEventLogPath(taskID)),
		Records: records,
	}
}

func timelineTelemetrySource(st *state.StateStore, taskID string, telemetryErr error, records int) timelineSourceState {
	if taskID == "" {
		return timelineSourceState{Status: statusNone}
	}
	status := timelineSourceOK
	if telemetryErr != nil {
		status = statusUnreadable
		if errors.Is(telemetryErr, os.ErrNotExist) {
			status = statusNone
		}
	}
	return timelineSourceState{
		Status:  status,
		Path:    timelineLocator(taskID, st.ModelCallLogPath(taskID)),
		Records: records,
	}
}

func timelineTaskStatsSource(st *state.StateStore, taskID string) timelineSourceState {
	if taskID == "" {
		return timelineSourceState{Status: statusNone}
	}
	if taskID == st.ReadOr("task.id", "") {
		return timelineCurrentTaskStatsSource(st)
	}
	return timelineArchivedTaskStatsSource(st, taskID)
}

func timelineCurrentTaskStatsSource(st *state.StateStore) timelineSourceState {
	_, err := st.CurrentTaskStats()
	status := timelineSourceOK
	if err != nil {
		status = statusUnreadable
		if errors.Is(err, os.ErrNotExist) {
			status = statusNone
		}
	}
	return timelineSourceState{Status: status, Path: st.CurrentTaskStatsPath()}
}

func timelineArchivedTaskStatsSource(st *state.StateStore, taskID string) timelineSourceState {
	evidence, err := st.ArchivedTaskStatsEvidence(taskID)
	sourceStatus := timelineSourceOK
	if errors.Is(err, os.ErrNotExist) || (err == nil && !evidence.Proven) {
		sourceStatus = statusNone
	} else if err != nil {
		sourceStatus = statusUnreadable
	}
	return timelineSourceState{Status: sourceStatus, Path: st.TaskStatsArchivePath(taskID)}
}

func timelineLocator(taskID string, path string) string {
	if taskID == "" {
		return ""
	}
	return path
}

func timelineTaskProven(aging []state.SessionAging, statsSource timelineSourceState) bool {
	return len(aging) > 0 || statsSource.Status == timelineSourceOK
}

func validTimelineTaskID(taskID string, explicit bool) bool {
	if taskID == "" && !explicit {
		return true
	}
	return state.ValidGeneratedUUID(taskID)
}

func timelineTaskStatus(st *state.StateStore, taskID string, explicit bool) *string {
	if !explicit {
		return taskStatusPtr(st.TaskStatus())
	}
	if taskID == st.ReadOr("task.id", "") {
		return taskStatusPtr(st.TaskStatus())
	}
	evidence, err := st.ArchivedTaskStatsEvidence(taskID)
	if err != nil || !evidence.Proven {
		return nil
	}
	return taskStatusPtr(evidence.Status)
}

func readTaskEventRecords(st *state.StateStore, taskID string) ([]state.TaskEventRecord, int, error) {
	return scanLogRecords(st.TaskEventLogPath(taskID), state.ParseTaskEventLine)
}

func readLastTaskEvent(path string) (state.TaskEventRecord, bool) {
	file, err := os.Open(path)
	if err != nil {
		return state.TaskEventRecord{}, false
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var last state.TaskEventRecord
	found := false
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			continue
		}
		last = record
		found = true
	}
	return last, found
}

func timelineCalls(records []state.TaskEventRecord) []timelineCall {
	entries := state.CallsFromTaskEvents(records)
	calls := make([]timelineCall, 0, len(entries))
	for index, entry := range entries {
		calls = append(calls, timelineCallDetail(index+1, entry))
	}
	return calls
}

func timelineCallDetail(index int, entry state.CallTimelineEntry) timelineCall {
	call := timelineCall{
		Index:            index,
		Role:             stringPtr(entry.Role),
		Phase:            stringPtr(entry.Phase),
		CallID:           entry.CallID,
		SessionID:        stringPtr(entry.SessionID),
		SessionCallIndex: entry.SessionCallIndex,
		Resumed:          entry.Resumed,
		ModelAlias:       stringPtr(entry.ModelAlias),
		MessageModel:     stringPtr(entry.MessageModel),
		Events:           entry.Events,
	}
	if !entry.FirstAt.IsZero() {
		firstAt := entry.FirstAt
		call.FirstAt = &firstAt
	}
	if !entry.LastAt.IsZero() {
		lastAt := entry.LastAt
		call.LastAt = &lastAt
	}
	if !entry.FirstAt.IsZero() && !entry.LastAt.IsZero() {
		span := entry.LastAt.Sub(entry.FirstAt).Milliseconds()
		call.SpanMS = &span
	}
	call.Result = timelineCallResultDetail(entry)
	call.Tools = timelineTools(entry.Tools)
	return call
}

func timelineCallResultDetail(entry state.CallTimelineEntry) timelineCallResult {
	result := timelineCallResult{Observed: entry.ResultObserved}
	if !entry.ResultObserved {
		return result
	}
	result.Subtype = stringPtr(entry.ResultSubtype)
	result.IsError = entry.IsError
	if entry.DurationMS > 0 {
		duration := entry.DurationMS
		result.DurationMS = &duration
	}
	if entry.DurationAPIMS > 0 {
		apiDuration := entry.DurationAPIMS
		result.APIDurationMS = &apiDuration
	}
	result.Turns = entry.NumTurns
	result.Usage = entry.Usage
	result.TotalCostUSD = entry.TotalCostUSD
	return result
}

func timelineTools(tools []state.CallTimelineTool) []timelineTool {
	rendered := make([]timelineTool, 0, len(tools))
	for _, tool := range tools {
		rendered = append(rendered, timelineTool{
			Name:          tool.Name,
			Uses:          tool.Uses,
			Results:       tool.Results,
			Measured:      tool.Measured,
			MeasuredSumMS: tool.MeasuredSumMS,
			MeasuredMaxMS: tool.MeasuredMaxMS,
			Unmeasured:    tool.Unmeasured,
			Errors:        tool.Errors,
		})
	}
	return rendered
}

func marshalEventLine(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
