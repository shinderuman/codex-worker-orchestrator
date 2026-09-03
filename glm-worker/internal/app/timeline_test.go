package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func timelineBaseTime() time.Time {
	return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
}

func executeTimelineOutput(t *testing.T, st *state.StateStore, taskID string) timelineOutput {
	t.Helper()
	var out bytes.Buffer
	if err := printTimeline(st, taskID, &out); err != nil {
		t.Fatal(err)
	}
	var output timelineOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("timeline出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	return output
}

func timelineToolOf(t *testing.T, tools []timelineTool, name string) timelineTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool集計に%qがありません: %#v", name, tools)
	return timelineTool{}
}

func timelineAgingOf(t *testing.T, agings []state.SessionAging, sessionID string) state.SessionAging {
	t.Helper()
	for _, aging := range agings {
		if aging.SessionID == sessionID {
			return aging
		}
	}
	t.Fatalf("session_agingに%qがありません: %#v", sessionID, agings)
	return state.SessionAging{}
}

func TestTimelineRendersCallsToolsGraphAndAging(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := timelineBaseTime()
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Seq: 1, Timestamp: base, Kind: "system", Subtype: "init", MessageModel: "glm-5.3"},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Seq: 2, Timestamp: base.Add(2 * time.Second), Kind: "assistant", MessageModel: "glm-5.3", Blocks: []state.TaskBlockSummary{
			{Type: "tool_use", Name: "Bash", ToolID: "t1", Bytes: 80},
			{Type: "tool_use", Name: "Bash", ToolID: "t2", Bytes: 81},
			{Type: "tool_use", Name: "Read", ToolID: "t3", Bytes: 60},
		}},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Seq: 3, Timestamp: base.Add(3 * time.Second), Kind: "user", Blocks: []state.TaskBlockSummary{
			{Type: "tool_result", Name: "Bash", ToolID: "t1", Bytes: 100, DurationMS: 500},
			{Type: "tool_result", Name: "Bash", ToolID: "t2", Bytes: 101, DurationMS: 700, IsError: true},
			{Type: "tool_result", ToolID: "t4", Bytes: 102},
		}},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Seq: 4, Timestamp: base.Add(9 * time.Second), Kind: "result", Subtype: "success", DurationMS: 9000, DurationAPIMS: 8000, NumTurns: 4, TotalCostUSD: 0.5, Usage: &state.TaskEventUsage{InputTokens: 100, CacheReadInputTokens: 20, OutputTokens: 30}},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-2", SessionID: "sess-b", Role: "reviewer", Phase: "reviewer-1", ModelAlias: "sonnet", Resumed: true, Seq: 1, Timestamp: base.Add(5 * time.Minute), Kind: "assistant", MessageModel: "glm-4.7"},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-2", SessionID: "sess-b", Role: "reviewer", Phase: "reviewer-1", ModelAlias: "sonnet", Resumed: true, Seq: 2, Timestamp: base.Add(5*time.Minute + 3*time.Second), Kind: "assistant", MessageModel: "glm-4.7"},
	)
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, SessionID: "sess-a", Role: state.WorkerRole,
		ModelAlias: "opus", StartedAt: base, CompletedAt: base.Add(9 * time.Second),
		TreeUsage:      state.TokenUsage{InputTokens: 100, CacheReadInputTokens: 20, OutputTokens: 30},
		WallDurationMS: 9000, TopLevelTurns: 4,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, SessionID: "sess-b", Role: state.ReviewerRole,
		ModelAlias: "sonnet", StartedAt: base.Add(5 * time.Minute), CompletedAt: base.Add(5*time.Minute + 3*time.Second),
		TreeUsage: state.TokenUsage{InputTokens: 50, OutputTokens: 5}, WallDurationMS: 3000,
	})

	output := executeTimelineOutput(t, st, "")
	if output.TaskID != taskID {
		t.Fatalf("task_id = %q", output.TaskID)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusActive) {
		t.Fatalf("task_status = %#v want %q", output.TaskStatus, state.TaskStatusActive)
	}
	if output.EventLog.Status != "ok" || output.EventLog.Path == nil || *output.EventLog.Path != st.TaskEventLogPath(taskID) {
		t.Fatalf("event_log = %#v", output.EventLog)
	}
	if len(output.Calls) != 2 {
		t.Fatalf("calls = %#v", output.Calls)
	}

	first := output.Calls[0]
	if first.Index != 1 || first.Role == nil || *first.Role != "worker" || first.Phase == nil || *first.Phase != "worker-new" ||
		first.SessionID == nil || *first.SessionID != "sess-a" || first.SessionCallIndex != 1 ||
		first.ModelAlias == nil || *first.ModelAlias != "opus" || first.MessageModel == nil || *first.MessageModel != "glm-5.3" || first.Resumed {
		t.Fatalf("call#1 = %#v", first)
	}
	if first.FirstAt == nil || !first.FirstAt.Equal(base) || first.LastAt == nil || !first.LastAt.Equal(base.Add(9*time.Second)) {
		t.Fatalf("call#1の観測窓 = %#v", first)
	}
	if first.SpanMS == nil || *first.SpanMS != 9000 || first.Events != 4 {
		t.Fatalf("call#1の窓数値 = %#v", first)
	}
	if !first.Result.Observed || first.Result.Subtype == nil || *first.Result.Subtype != "success" ||
		first.Result.DurationMS == nil || *first.Result.DurationMS != 9000 || first.Result.APIDurationMS == nil || *first.Result.APIDurationMS != 8000 ||
		first.Result.Turns != 4 || first.Result.TotalCostUSD != 0.5 {
		t.Fatalf("call#1のresult = %#v", first.Result)
	}
	if first.Result.Usage == nil || first.Result.Usage.InputTokens != 100 || first.Result.Usage.CacheReadInputTokens != 20 || first.Result.Usage.OutputTokens != 30 {
		t.Fatalf("call#1のusage = %#v", first.Result.Usage)
	}
	bash := timelineToolOf(t, first.Tools, "Bash")
	if bash.Uses != 2 || bash.Results != 2 || bash.Measured != 2 || bash.MeasuredSumMS != 1200 || bash.MeasuredMaxMS != 700 || bash.Errors != 1 {
		t.Fatalf("call#1のBash集計 = %#v", bash)
	}
	if read := timelineToolOf(t, first.Tools, "Read"); read.Uses != 1 || read.Results != 0 {
		t.Fatalf("call#1のRead集計 = %#v", read)
	}
	if unknown := timelineToolOf(t, first.Tools, "unknown"); unknown.Uses != 0 || unknown.Results != 1 || unknown.Unmeasured != 1 {
		t.Fatalf("call#1のunknown集計 = %#v", unknown)
	}

	second := output.Calls[1]
	if second.Index != 2 || second.Role == nil || *second.Role != "reviewer" || second.Phase == nil || *second.Phase != "reviewer-1" ||
		second.SessionID == nil || *second.SessionID != "sess-b" || !second.Resumed ||
		second.ModelAlias == nil || *second.ModelAlias != "sonnet" || second.MessageModel == nil || *second.MessageModel != "glm-4.7" {
		t.Fatalf("call#2 = %#v", second)
	}
	if second.SpanMS == nil || *second.SpanMS != 3000 || second.Events != 2 {
		t.Fatalf("call#2の窓数値 = %#v", second)
	}
	if second.Result.Observed {
		t.Fatalf("call#2のresult = %#v", second.Result)
	}
	if len(second.Tools) != 0 {
		t.Fatalf("call#2のtools = %#v", second.Tools)
	}

	bashTotal := timelineToolOf(t, output.ToolTotals, "Bash")
	if bashTotal.Uses != 2 || bashTotal.Results != 2 || bashTotal.Measured != 2 || bashTotal.MeasuredSumMS != 1200 || bashTotal.MeasuredMaxMS != 700 || bashTotal.Errors != 1 {
		t.Fatalf("tool_totalsのBash = %#v", bashTotal)
	}
	if unknownTotal := timelineToolOf(t, output.ToolTotals, "unknown"); unknownTotal.Results != 1 || unknownTotal.Unmeasured != 1 {
		t.Fatalf("tool_totalsのunknown = %#v", unknownTotal)
	}

	workerAging := timelineAgingOf(t, output.SessionAging, "sess-a")
	if workerAging.Role != state.WorkerRole || workerAging.Calls != 1 || workerAging.ResumedCalls != 0 ||
		workerAging.CumulativeTurns != 4 || workerAging.CumulativeInputTokens != 120 || workerAging.CumulativeOutputTokens != 30 {
		t.Fatalf("sess-a aging = %#v", workerAging)
	}
	if len(workerAging.Models) != 1 || workerAging.Models[0] != "opus" {
		t.Fatalf("sess-a agingのmodels = %#v", workerAging.Models)
	}
	if len(workerAging.CallLatencyMS) != 1 || workerAging.CallLatencyMS[0] != 9000 {
		t.Fatalf("sess-a agingのlatency = %#v", workerAging.CallLatencyMS)
	}
	reviewerAging := timelineAgingOf(t, output.SessionAging, "sess-b")
	if reviewerAging.Role != state.ReviewerRole || reviewerAging.Calls != 1 || reviewerAging.ResumedCalls != 0 ||
		reviewerAging.CumulativeTurns != 0 || reviewerAging.CumulativeInputTokens != 50 || reviewerAging.CumulativeOutputTokens != 5 {
		t.Fatalf("sess-b aging = %#v", reviewerAging)
	}
	if len(reviewerAging.CallLatencyMS) != 1 || reviewerAging.CallLatencyMS[0] != 3000 {
		t.Fatalf("sess-b agingのlatency = %#v", reviewerAging.CallLatencyMS)
	}
}

func TestTimelineSkipsCorruptLines(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := timelineBaseTime()
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Timestamp: base, Kind: "assistant"},
	)
	path := st.TaskEventLogPath(taskID)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"version\":1,\"kind\":\"brokencorrupt\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Timestamp: base.Add(time.Second), Kind: "result", Subtype: "success", DurationMS: 1000},
	)

	output := executeTimelineOutput(t, st, "")
	if output.SkippedEvents != 1 {
		t.Fatalf("skipped_events = %d", output.SkippedEvents)
	}
	if len(output.Calls) != 1 {
		t.Fatalf("calls = %#v", output.Calls)
	}
	result := output.Calls[0].Result
	if !result.Observed || result.Subtype == nil || *result.Subtype != "success" || result.DurationMS == nil || *result.DurationMS != 1000 {
		t.Fatalf("破損行以後のresult = %#v", result)
	}
}

func TestTimelineCurrentTaskWithoutEvents(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: taskID, CallType: state.CallTypeTask, SessionID: "sess-a", Role: state.WorkerRole,
		ModelAlias: "opus", StartedAt: timelineBaseTime(), CompletedAt: timelineBaseTime().Add(time.Second),
		WallDurationMS: 1000,
	})

	output := executeTimelineOutput(t, st, "")
	if output.TaskID != taskID {
		t.Fatalf("task_id = %q", output.TaskID)
	}
	if output.EventLog.Status != "none" || output.EventLog.Path != nil {
		t.Fatalf("event_log = %#v", output.EventLog)
	}
	aging := timelineAgingOf(t, output.SessionAging, "sess-a")
	if aging.Role != state.WorkerRole || aging.Calls != 1 {
		t.Fatalf("sess-a aging = %#v", aging)
	}
	if output.Calls != nil || output.ToolTotals != nil {
		t.Fatalf("event logがないのにcall表示 = %#v", output)
	}
	if output.Coverage.Status != timelineStatusPartial || len(output.Coverage.MissingSources) != 1 || output.Coverage.MissingSources[0] != "event_log" {
		t.Fatalf("event logなしtelemetryありのcoverage = %#v", output.Coverage)
	}
}

func TestTimelineExplicitTask(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := timelineBaseTime()
	writeTaskEventLines(t, st, oldTaskID,
		state.TaskEventRecord{TaskID: oldTaskID, CallID: "call-1", SessionID: "sess-old", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Timestamp: base, Kind: "result", Subtype: "success", DurationMS: 4000},
	)
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID: oldTaskID, CallType: state.CallTypeTask, SessionID: "sess-old", Role: state.WorkerRole,
		ModelAlias: "opus", StartedAt: base, CompletedAt: base.Add(4 * time.Second), WallDurationMS: 4000,
	})
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	output := executeTimelineOutput(t, st, oldTaskID)
	if output.TaskID != oldTaskID {
		t.Fatalf("task_id = %q", output.TaskID)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusComplete) {
		t.Fatalf("task_status = %#v want %q", output.TaskStatus, state.TaskStatusComplete)
	}
	if len(output.Calls) != 1 {
		t.Fatalf("calls = %#v", output.Calls)
	}
	call := output.Calls[0]
	if call.Role == nil || *call.Role != "worker" || call.Phase == nil || *call.Phase != "worker-new" ||
		call.SessionID == nil || *call.SessionID != "sess-old" || call.SessionCallIndex != 1 {
		t.Fatalf("call = %#v", call)
	}
	if !call.Result.Observed || call.Result.Subtype == nil || *call.Result.Subtype != "success" || call.Result.DurationMS == nil || *call.Result.DurationMS != 4000 {
		t.Fatalf("result = %#v", call.Result)
	}
	aging := timelineAgingOf(t, output.SessionAging, "sess-old")
	if aging.Role != state.WorkerRole || aging.Calls != 1 {
		t.Fatalf("sess-old aging = %#v", aging)
	}
	if output.Coverage.Status != timelineStatusComplete || output.Coverage.MissingSources != nil {
		t.Fatalf("event logありのcoverage = %#v", output.Coverage)
	}
	if output.Coverage.Sources.EventLog.Status != timelineSourceOK || output.Coverage.Sources.EventLog.Records != 1 ||
		output.Coverage.Sources.EventLog.Path != st.TaskEventLogPath(oldTaskID) {
		t.Fatalf("event logありのcoverage.sources.event_log = %#v", output.Coverage.Sources.EventLog)
	}
	if output.Coverage.Sources.TaskStats.Status != timelineSourceOK || output.Coverage.Sources.TaskStats.Path != st.TaskStatsArchivePath(oldTaskID) {
		t.Fatalf("event logありのcoverage.sources.task_stats = %#v", output.Coverage.Sources.TaskStats)
	}

	out := &bytes.Buffer{}
	if err := printTimeline(st, "12345678-1234-4234-8123-123456789abc", out); err == nil {
		t.Fatalf("存在しないtask IDがerrorになりません: %s", out.String())
	}
}

func retainTimelineTask(t *testing.T, st *state.StateStore, record func(taskID string)) string {
	t.Helper()
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		record(taskID)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.TaskEventLogPath(taskID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return taskID
}

func TestTimelineRetainedTaskReturnsPartialTelemetryTimeline(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := timelineBaseTime()
	oldTaskID := retainTimelineTask(t, st, func(taskID string) {
		st.RecordModelCallLog(state.ModelCallLog{
			TaskID: taskID, CallType: state.CallTypeTask, SessionID: "sess-old", Role: state.WorkerRole,
			ModelAlias: "opus", StartedAt: base, CompletedAt: base.Add(4 * time.Second), WallDurationMS: 4000,
		})
	})

	var out bytes.Buffer
	if err := printTimeline(st, oldTaskID, &out); err != nil {
		t.Fatalf("retention済みtaskが全体失敗しました: %v", err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	for _, key := range []string{"task_id", "coverage", "session_aging", "event_log"} {
		requireJSONKey(t, decoded, key)
	}
	var output timelineOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("timeline出力がmachine JSONではありません: %v: %q", err, out.String())
	}

	if output.Coverage.Status != timelineStatusPartial {
		t.Fatalf("coverage.status = %#v", output.Coverage)
	}
	if len(output.Coverage.MissingSources) != 1 || output.Coverage.MissingSources[0] != "event_log" {
		t.Fatalf("coverage.missing_sources = %#v", output.Coverage.MissingSources)
	}
	if output.Coverage.Sources.EventLog.Status != statusNone || output.Coverage.Sources.EventLog.Records != 0 ||
		output.Coverage.Sources.EventLog.Path != st.TaskEventLogPath(oldTaskID) {
		t.Fatalf("coverage.sources.event_log = %#v", output.Coverage.Sources.EventLog)
	}
	if output.Coverage.Sources.Telemetry.Status != timelineSourceOK || output.Coverage.Sources.Telemetry.Records != 1 ||
		output.Coverage.Sources.Telemetry.Path != st.ModelCallLogPath(oldTaskID) {
		t.Fatalf("coverage.sources.telemetry = %#v", output.Coverage.Sources.Telemetry)
	}
	if output.Coverage.Sources.TaskStats.Status != timelineSourceOK || output.Coverage.Sources.TaskStats.Path != st.TaskStatsArchivePath(oldTaskID) {
		t.Fatalf("coverage.sources.task_stats = %#v", output.Coverage.Sources.TaskStats)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusComplete) {
		t.Fatalf("task_status = %#v", output.TaskStatus)
	}
	if output.EventLog.Status != statusNone {
		t.Fatalf("event_log = %#v", output.EventLog)
	}
	aging := timelineAgingOf(t, output.SessionAging, "sess-old")
	if aging.Role != state.WorkerRole || aging.Calls != 1 {
		t.Fatalf("sess-old aging = %#v", aging)
	}
	if output.Calls != nil || output.ToolTotals != nil {
		t.Fatalf("event logがないのにcall表示 = %#v", output)
	}
}

func TestTimelineRetainedTaskWithoutRecordsReturnsUnknownTimeline(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldTaskID := retainTimelineTask(t, st, nil)

	output := executeTimelineOutput(t, st, oldTaskID)
	if output.Coverage.Status != timelineStatusUnknown {
		t.Fatalf("coverage.status = %#v", output.Coverage)
	}
	if len(output.Coverage.MissingSources) != 2 ||
		output.Coverage.MissingSources[0] != "event_log" || output.Coverage.MissingSources[1] != "telemetry" {
		t.Fatalf("coverage.missing_sources = %#v", output.Coverage.MissingSources)
	}
	if output.Coverage.Sources.Telemetry.Status != statusNone || output.Coverage.Sources.Telemetry.Path != st.ModelCallLogPath(oldTaskID) {
		t.Fatalf("coverage.sources.telemetry = %#v", output.Coverage.Sources.Telemetry)
	}
	if output.Coverage.Sources.TaskStats.Status != timelineSourceOK {
		t.Fatalf("coverage.sources.task_stats = %#v", output.Coverage.Sources.TaskStats)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusComplete) {
		t.Fatalf("task_status = %#v", output.TaskStatus)
	}
	if output.Telemetry == nil || *output.Telemetry != timelineSourceOK {
		t.Fatalf("telemetry = %#v", output.Telemetry)
	}
	if len(output.SessionAging) != 0 || output.Calls != nil {
		t.Fatalf("recordがないのに表示 = %#v", output)
	}
}

func TestTimelineRetainedTaskWithMalformedTelemetryReportsUnreadable(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldTaskID := retainTimelineTask(t, st, nil)
	telemetryPath := st.ModelCallLogPath(oldTaskID)
	if err := os.MkdirAll(filepath.Dir(telemetryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(telemetryPath, []byte("{\"version\":3,\"broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeTimelineOutput(t, st, oldTaskID)
	if output.Coverage.Status != timelineStatusUnknown {
		t.Fatalf("coverage.status = %#v", output.Coverage)
	}
	if output.Coverage.Sources.Telemetry.Status != statusUnreadable {
		t.Fatalf("coverage.sources.telemetry = %#v", output.Coverage.Sources.Telemetry)
	}
	if len(output.Coverage.MissingSources) != 1 || output.Coverage.MissingSources[0] != "event_log" {
		t.Fatalf("coverage.missing_sources = %#v", output.Coverage.MissingSources)
	}
	if output.Telemetry == nil || *output.Telemetry != statusUnreadable {
		t.Fatalf("telemetry = %#v", output.Telemetry)
	}
	if len(output.SessionAging) != 0 {
		t.Fatalf("malformed telemetryからagingを生成しました: %#v", output.SessionAging)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusComplete) {
		t.Fatalf("task_status = %#v", output.TaskStatus)
	}
}

func retainedStatsArchiveJSON(version int, taskID string, status state.TaskStatus) string {
	return fmt.Sprintf(`{
  "version": %d,
  "task_id": %q,
  "started_at": "2026-09-01T16:24:06.607326Z",
  "archived_at": "2026-09-01T21:01:31.686938Z",
  "status": %q,
  "parent_codex_thread_id": "01a05dc7-d229-79c0-8173-6a35081f6a05",
  "model_calls": 8,
  "worker_calls": 6,
  "reviewer_calls": 2
}
`, version, taskID, status)
}

func writeRetainedStatsArchive(t *testing.T, st *state.StateStore, taskID string, archive string) {
	t.Helper()
	if err := os.WriteFile(st.TaskStatsArchivePath(taskID), []byte(archive), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTimelineStatsOnlyRetainedTaskProvesTaskFromVersionThreeArchive(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldTaskID := retainTimelineTask(t, st, nil)
	for _, status := range []state.TaskStatus{state.TaskStatusComplete, state.TaskStatusWaitingSolReview} {
		writeRetainedStatsArchive(t, st, oldTaskID, retainedStatsArchiveJSON(3, oldTaskID, status))
		var out bytes.Buffer
		if err := printTimeline(st, oldTaskID, &out); err != nil {
			t.Fatalf("stats-only retained taskが全体失敗しました(%s): %v", status, err)
		}
		decoded := decodeSingleLineJSON(t, out.String())
		requireJSONKey(t, decoded, "task_status")
		requireJSONKey(t, decoded, "coverage")
		var output timelineOutput
		if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
			t.Fatalf("timeline出力がmachine JSONではありません: %v: %q", err, out.String())
		}
		if output.TaskStatus == nil || *output.TaskStatus != string(status) {
			t.Fatalf("task_status = %#v want %q", output.TaskStatus, status)
		}
		if output.Coverage.Sources.TaskStats.Status != timelineSourceOK || output.Coverage.Sources.TaskStats.Path != st.TaskStatsArchivePath(oldTaskID) {
			t.Fatalf("coverage.sources.task_stats = %#v", output.Coverage.Sources.TaskStats)
		}
		if output.Coverage.Status != timelineStatusUnknown {
			t.Fatalf("coverage.status = %#v", output.Coverage)
		}
		if len(output.Coverage.MissingSources) != 2 ||
			output.Coverage.MissingSources[0] != "event_log" || output.Coverage.MissingSources[1] != "telemetry" {
			t.Fatalf("coverage.missing_sources = %#v", output.Coverage.MissingSources)
		}
		if output.Calls != nil || output.ToolTotals != nil {
			t.Fatalf("event logがないのにcall表示 = %#v", output)
		}
	}

	writeRetainedStatsArchive(t, st, oldTaskID, retainedStatsArchiveJSON(3, oldTaskID, "mysterious-status"))
	unknownOut := &bytes.Buffer{}
	if err := printTimeline(st, oldTaskID, unknownOut); err == nil {
		t.Fatalf("未知statusのstats-only archiveが存在証拠や成功JSONへ昇格しました: %s", unknownOut.String())
	}
	if body := unknownOut.String(); body != "" {
		t.Fatalf("失敗時にstdoutへ出力しました: %s", body)
	}
}

func TestTimelineRetainedTaskWithUnusableStatsArchiveReportsSourceState(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := timelineBaseTime()
	oldTaskID := retainTimelineTask(t, st, func(taskID string) {
		st.RecordModelCallLog(state.ModelCallLog{
			TaskID: taskID, CallType: state.CallTypeTask, SessionID: "sess-old", Role: state.WorkerRole,
			ModelAlias: "opus", StartedAt: base, CompletedAt: base.Add(4 * time.Second), WallDurationMS: 4000,
		})
	})
	fixtures := []struct {
		name            string
		archive         string
		wantStatsStatus string
		wantMissing     []string
	}{
		{"malformed", "{\"version\":3,\"broken\n", statusUnreadable, []string{"event_log"}},
		{"status-type-wrong", fmt.Sprintf("{\n  \"version\": 3,\n  \"task_id\": %q,\n  \"status\": 12\n}\n", oldTaskID), statusUnreadable, []string{"event_log"}},
		{"status-missing", fmt.Sprintf("{\n  \"version\": 3,\n  \"task_id\": %q,\n  \"started_at\": \"2026-09-01T16:24:06.607326Z\"\n}\n", oldTaskID), statusUnreadable, []string{"event_log"}},
		{"status-unknown", retainedStatsArchiveJSON(3, oldTaskID, "mysterious-status"), statusUnreadable, []string{"event_log"}},
		{"future-schema-revision", fmt.Sprintf("{\n  \"version\": 3,\n  \"schema_revision\": 2,\n  \"task_id\": %q,\n  \"status\": %q\n}\n", oldTaskID, state.TaskStatusComplete), statusUnreadable, []string{"event_log"}},
		{"unsupported-version", retainedStatsArchiveJSON(2, oldTaskID, state.TaskStatusComplete), statusNone, []string{"event_log", "task_stats"}},
		{"foreign-task", retainedStatsArchiveJSON(3, "11111111-2222-4333-8444-555555555555", state.TaskStatusComplete), statusNone, []string{"event_log", "task_stats"}},
	}
	for _, fixture := range fixtures {
		writeRetainedStatsArchive(t, st, oldTaskID, fixture.archive)
		output := executeTimelineOutput(t, st, oldTaskID)
		if output.Coverage.Sources.TaskStats.Status != fixture.wantStatsStatus {
			t.Fatalf("%sのcoverage.sources.task_stats = %#v want %q", fixture.name, output.Coverage.Sources.TaskStats, fixture.wantStatsStatus)
		}
		if output.TaskStatus != nil {
			t.Fatalf("%sのtask_status = %#v want nil", fixture.name, output.TaskStatus)
		}
		if len(output.Coverage.MissingSources) != len(fixture.wantMissing) {
			t.Fatalf("%sのcoverage.missing_sources = %#v want %v", fixture.name, output.Coverage.MissingSources, fixture.wantMissing)
		}
		for index, source := range fixture.wantMissing {
			if output.Coverage.MissingSources[index] != source {
				t.Fatalf("%sのcoverage.missing_sources = %#v want %v", fixture.name, output.Coverage.MissingSources, fixture.wantMissing)
			}
		}
		if aging := timelineAgingOf(t, output.SessionAging, "sess-old"); aging.Calls != 1 {
			t.Fatalf("%sのsess-old aging = %#v", fixture.name, aging)
		}
	}
}

func writeTimelineSentinel(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	sentinel := state.TaskEventRecord{
		Version: 1, TaskID: "evil", CallID: "sentinel-call", Role: "sentinel-role",
		Phase: "sentinel-phase", Timestamp: timelineBaseTime(), Kind: "assistant",
	}
	data, err := json.Marshal(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.StateBase, "evil.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTimelineRejectsTaskIDOutsideGeneratedForm(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeTimelineSentinel(t, cfg)

	for _, taskID := range []string{
		"../../evil",
		"../../../sessions/other/events/x",
		"/etc/hostname",
		"12345678-1234-1234-8123-123456789abc",
		"12345678-1234-4234-c123-123456789abc",
		"12345678-1234-4234-8123-123456789ABC",
		"none",
	} {
		out := &bytes.Buffer{}
		if err := printTimeline(st, taskID, out); err == nil {
			t.Fatalf("不正task ID %qがerrorになりません: %s", taskID, out.String())
		}
		if body := out.String(); body != "" {
			t.Fatalf("不正task ID %qが出力しました: %s", taskID, body)
		}
		if strings.Contains(out.String(), "sentinel-role") {
			t.Fatalf("state root外のsentinelが読まれました: %s", out.String())
		}
	}
}

func TestTimelineRejectsTamperedCurrentTaskID(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeTimelineSentinel(t, cfg)
	if err := st.Write("task.id", "../../evil"); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	if err := printTimeline(st, "", out); err == nil {
		t.Fatalf("改変task.idがerrorになりません: %s", out.String())
	}
	if body := out.String(); body != "" || strings.Contains(body, "sentinel-role") {
		t.Fatalf("改変task.idで出力またはsentinel読取がありました: %q", body)
	}
}

func TestExecuteTimelineRejectsTraversal(t *testing.T) {
	cfg := newAppConfig(t)
	writeTimelineSentinel(t, cfg)
	cmd, err := ParseCommand([]string{"--timeline", "../../evil"})
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err == nil {
		t.Fatalf("traversal task IDがerrorになりません: %s", out.String())
	}
	if body := out.String(); body != "" || strings.Contains(body, "sentinel-role") {
		t.Fatalf("traversal task IDで出力またはsentinel読取がありました: %q", body)
	}
}

func TestExecuteTimelineDoesNotCreateState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "timelinehash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--timeline"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeTimeline || cmd.Payload != "" {
		t.Fatalf("command = %+v", cmd)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output timelineOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("timeline出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.TaskID != "" || output.EventLog.Status != "none" {
		t.Fatalf("timeline出力 = %#v", output)
	}
	if output.Coverage.Status != timelineStatusUnknown {
		t.Fatalf("task不在のcoverage = %#v", output.Coverage)
	}
	if len(output.Coverage.MissingSources) != 3 ||
		output.Coverage.Sources.EventLog.Path != "" || output.Coverage.Sources.Telemetry.Path != "" || output.Coverage.Sources.TaskStats.Path != "" {
		t.Fatalf("task不在のcoverage sources = %#v", output.Coverage)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--timelineがstate dirを作成しました: %v", entries)
	}
}

func TestParseCommandTimeline(t *testing.T) {
	cmd, err := ParseCommand([]string{"--timeline", "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeTimeline || cmd.Payload != "task-1" {
		t.Fatalf("command = %+v", cmd)
	}
	if _, err := ParseCommand([]string{"--timeline", "task-1", "extra"}); err == nil {
		t.Fatal("余分な引数が受け入れられています")
	}
}
