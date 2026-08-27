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
	EventLog      timelineEventLog     `json:"event_log"`
	SkippedEvents int                  `json:"skipped_events,omitempty"`
	Telemetry     *string              `json:"telemetry"`
	Calls         []timelineCall       `json:"calls"`
	ToolTotals    []timelineTool       `json:"tool_totals"`
	SessionAging  []state.SessionAging `json:"session_aging"`
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

func printTimeline(st *state.StateStore, taskIDArg string, stdout io.Writer) error {
	explicit := taskIDArg != ""
	taskID := taskIDArg
	if taskID == "" {
		taskID = st.ReadOr("task.id", "")
	}
	if !validTimelineTaskID(taskID, explicit) {
		return &UsageError{Message: fmt.Sprintf("task IDが生成されるUUID v4形式と一致しません: %q", taskID)}
	}

	output := timelineOutput{
		TaskID:     taskID,
		TaskStatus: timelineTaskStatus(st, taskID, explicit),
	}
	records, skipped, err := readTaskEventRecords(st, taskID)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if explicit {
			return &NotFoundError{Message: fmt.Sprintf("task %sのevent logがありません: %v", taskID, err)}
		}
		output.EventLog = timelineEventLog{Status: statusNone}
	case err != nil:
		if explicit {
			return fmt.Errorf("task %sのevent logを読めません: %w", taskID, err)
		}
		output.EventLog = timelineEventLog{Status: statusUnreadable}
	default:
		output.EventLog = timelineEventLog{Status: "ok", Path: stringPtr(st.TaskEventLogPath(taskID))}
		output.Calls = timelineCalls(records)
		output.ToolTotals = timelineTools(state.SumCallTimelineTools(state.CallsFromTaskEvents(records)))
		output.SkippedEvents = skipped
	}

	logs, logErr := readStatusTelemetry(st, taskID)
	fillTimelineTelemetry(taskID, logErr, logs, &output)
	return writeJSON(stdout, output)
}

func fillTimelineTelemetry(taskID string, logErr error, logs []state.ModelCallLog, output *timelineOutput) {
	if taskID == "" {
		return
	}
	if logErr != nil {
		unreadable := statusUnreadable
		output.Telemetry = &unreadable
		return
	}
	ok := "ok"
	output.Telemetry = &ok
	output.SessionAging = state.AgingFromModelCallLogs(logs)
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
	all, err := st.AllTaskStats()
	if err != nil {
		return nil
	}
	for _, stats := range all {
		if stats.TaskID == taskID {
			return taskStatusPtr(stats.Status)
		}
	}
	return nil
}

func readTaskEventRecords(st *state.StateStore, taskID string) ([]state.TaskEventRecord, int, error) {
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var records []state.TaskEventRecord
	skipped := 0
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			skipped++
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return records, skipped, err
	}
	return records, skipped, nil
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
