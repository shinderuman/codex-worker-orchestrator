package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeTaskEventLines(t *testing.T, st *state.StateStore, taskID string, records ...state.TaskEventRecord) {
	t.Helper()
	for _, record := range records {
		if err := st.AppendTaskEvent(record); err != nil {
			t.Fatal(err)
		}
	}
}

func watchTestOptions(verbose bool, followInterval time.Duration, stop <-chan struct{}) watchOptions {
	return watchOptions{
		verbose:        verbose,
		followInterval: followInterval,
		statusInterval: defaultWatchStatusInterval,
		changeInterval: defaultWatchChangeInterval,
		now:            time.Now,
		stop:           stop,
	}
}

func watchTestStore(t *testing.T) (*state.StateStore, config.AppConfig) {
	t.Helper()
	cfg := config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "watchhash",
		RepoRoot:  "/repo",
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	return st, cfg
}

func parseWatchEvents(t *testing.T, rendered string) []map[string]any {
	t.Helper()
	events := []map[string]any{}
	for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("watch出力にJSONLでない行があります: %v: %q", err, line)
		}
		events = append(events, event)
	}
	return events
}

func watchEventIndex(events []map[string]any, eventType string) int {
	for i, event := range events {
		if event["type"] == eventType {
			return i
		}
	}
	return -1
}

func requireWatchEvent(t *testing.T, events []map[string]any, eventType string) map[string]any {
	t.Helper()
	index := watchEventIndex(events, eventType)
	if index < 0 {
		t.Fatalf("watch streamに%q eventがありません: %v", eventType, events)
	}
	return events[index]
}

func watchString(t *testing.T, event map[string]any, key string) string {
	t.Helper()
	value, ok := event[key].(string)
	if !ok {
		t.Fatalf("event %vの%qがJSON stringではありません: %#v", event["type"], key, event[key])
	}
	return value
}

func TestWatchRendersSavedEventsWithoutSideEffects(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init", MessageModel: "glm-5.3"},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "assistant", MessageModel: "glm-5.3", Blocks: []state.TaskBlockSummary{{Type: "thinking", Bytes: 456}, {Type: "tool_use", Name: "Bash", ToolID: "toolu_1", Bytes: 88}}, Usage: &state.TaskEventUsage{InputTokens: 100, OutputTokens: 7}},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-2", Role: "reviewer", Phase: "reviewer-1", Resumed: true, Kind: "result", Subtype: "success", NumTurns: 3, TotalCostUSD: 0.25, DurationMS: 1500, Usage: &state.TaskEventUsage{OutputTokens: 20}},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-2", Role: "reviewer", Phase: "reviewer-1", Kind: "user", Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Read", ToolID: "toolu_1", Bytes: 814, DurationMS: 456}}},
	)

	entriesBefore, err := os.ReadDir(st.Path("."))
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	if err := printWatch(st, out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	events := parseWatchEvents(t, out.String())
	start := requireWatchEvent(t, events, "watch_start")
	if watchString(t, start, "task_id") != taskID || watchString(t, start, "event_log") != st.TaskEventLogPath(taskID) {
		t.Fatalf("watch_start = %#v", start)
	}
	if watchString(t, start, "event_log_status") != "following" {
		t.Fatalf("event_log_status = %v", start["event_log_status"])
	}

	if len(events) != 5 {
		t.Fatalf("watch stream = %d events: %v", len(events), events)
	}
	assistant := events[2]
	if watchString(t, assistant, "kind") != "assistant" || watchString(t, assistant, "message_model") != "glm-5.3" {
		t.Fatalf("assistant record = %#v", assistant)
	}
	usage := assistant["usage"].(map[string]any)
	if usage["input_tokens"] != float64(100) || usage["output_tokens"] != float64(7) {
		t.Fatalf("assistant usage = %#v", usage)
	}
	result := events[3]
	if resumed, _ := result["resumed"].(bool); !resumed {
		t.Fatalf("result recordのresumed = %#v", result["resumed"])
	}
	if result["num_turns"] != float64(3) || result["total_cost_usd"] != float64(0.25) || result["duration_ms"] != float64(1500) {
		t.Fatalf("result record観測値 = %#v", result)
	}
	if strings.Contains(out.String(), "GLM_WORKER_PROBE_OK") {
		t.Fatalf("watch表示 = %q", out.String())
	}

	entriesAfter, err := os.ReadDir(st.Path("."))
	if err != nil {
		t.Fatal(err)
	}
	if len(entriesBefore) != len(entriesAfter) {
		t.Fatalf("watchがstate dirを変更しました: %d -> %d", len(entriesBefore), len(entriesAfter))
	}
	if st.Exists("lock") {
		t.Fatal("watchがrepo lockを作成しました")
	}
}

func TestWatchSkipsCorruptLines(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "assistant"},
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
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 2, Kind: "result", Subtype: "success"},
	)

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	if err := printWatch(st, out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	events := parseWatchEvents(t, out.String())
	skipped := requireWatchEvent(t, events, "event_skipped")
	if watchString(t, skipped, "error") == "" {
		t.Fatalf("event_skippedのerrorが空です: %#v", skipped)
	}
	if watchEventIndex(events, "event_skipped") != 2 {
		t.Fatalf("破損行のskip位置が不正です: %v", events)
	}
	if watchString(t, events[3], "kind") != "result" || watchString(t, events[3], "subtype") != "success" {
		t.Fatalf("破損行以後のrecordが流れていません: %#v", events[3])
	}
}

func TestWatchFollowsAppendedEvents(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)

	stop := make(chan struct{})
	out := &bytes.Buffer{}
	rendered := make(chan string, 1)
	go func() {
		err := printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, stop))
		if err != nil {
			t.Error(err)
		}
		rendered <- out.String()
	}()

	time.Sleep(20 * time.Millisecond)
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 2, Kind: "result", Subtype: "success", NumTurns: 2},
	)
	time.Sleep(30 * time.Millisecond)
	close(stop)
	events := parseWatchEvents(t, <-rendered)

	init := events[1]
	if watchString(t, init, "kind") != "system" || watchString(t, init, "subtype") != "init" {
		t.Fatalf("既存行が流れていません: %#v", init)
	}
	appended := events[2]
	if watchString(t, appended, "kind") != "result" || appended["num_turns"] != float64(2) {
		t.Fatalf("追記行のfollow表示がありません: %#v", appended)
	}
}

func TestWatchWithoutTaskOrLog(t *testing.T) {
	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "watchhash", RepoRoot: "/repo"}

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	if err := printWatch(state.AttachStateStore(cfg), out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	events := parseWatchEvents(t, out.String())
	start := requireWatchEvent(t, events, "watch_start")
	if start["task_id"] != nil || start["event_log"] != nil {
		t.Fatalf("task不在時のwatch_start = %#v", start)
	}
	if watchString(t, start, "event_log_status") != "none" {
		t.Fatalf("task不在時のevent_log_status = %v", start["event_log_status"])
	}

	st, _ := watchTestStore(t)
	out = &bytes.Buffer{}
	if err := printWatch(st, out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	events = parseWatchEvents(t, out.String())
	start = requireWatchEvent(t, events, "watch_start")
	if watchString(t, start, "event_log_status") != "empty" {
		t.Fatalf("log不在時のevent_log_status = %v", start["event_log_status"])
	}
}

func TestExecuteWatchDoesNotCreateState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "watchhash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--watch"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeWatch {
		t.Fatalf("mode = %v", cmd.Mode)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	events := parseWatchEvents(t, out.String())
	if watchString(t, requireWatchEvent(t, events, "watch_start"), "event_log_status") != "none" {
		t.Fatalf("watch出力 = %v", events)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--watchがstate dirを作成しました: %v", entries)
	}
}

func TestParseCommandWatchRejectsExtraArgs(t *testing.T) {
	if _, err := ParseCommand([]string{"--watch", "extra"}); err == nil {
		t.Fatal("余分な引数が受け入れられています")
	}
}

func runWatchUntilExit(t *testing.T, st *state.StateStore, followInterval time.Duration) string {
	t.Helper()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, watchTestOptions(false, followInterval, nil))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchが終端で終了しません")
	}
	return out.String()
}

func TestWatchExitsImmediatelyWhenTaskAlreadyNonActive(t *testing.T) {
	for _, status := range []state.TaskStatus{
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusComplete,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
	} {
		t.Run(string(status), func(t *testing.T) {
			st, _ := watchTestStore(t)
			taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
			writeTaskEventLines(t, st, taskID,
				state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "result", Subtype: "success"},
			)
			if err := st.SetTaskStatus(status); err != nil {
				t.Fatal(err)
			}

			events := parseWatchEvents(t, runWatchUntilExit(t, st, time.Millisecond))
			exit := requireWatchEvent(t, events, "watch_exit")
			if watchString(t, exit, "task_id") != taskID || watchString(t, exit, "status") != string(status) {
				t.Fatalf("watch_exit = %#v", exit)
			}
			if watchEventIndex(events, "watch_exit") != len(events)-1 {
				t.Fatalf("終端eventより後に出力があります: %v", events)
			}
			if watchEventIndex(events, "watch_exit") < watchEventIndex(events, "watch_start")+1 {
				t.Fatalf("保存済みeventより先に終端eventが出ています: %v", events)
			}
		})
	}
}

func TestWatchFollowsUntilStatusLeavesActive(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)

	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, nil))
	}()
	time.Sleep(20 * time.Millisecond)
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-2", Role: "reviewer", Phase: "reviewer-1", Kind: "result", Subtype: "success"},
	)
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchがnon-active遷移後に終了しません")
	}

	events := parseWatchEvents(t, out.String())
	if watchString(t, events[1], "kind") != "system" {
		t.Fatalf("既存行が流れていません: %#v", events[1])
	}
	reviewer := events[2]
	if watchString(t, reviewer, "phase") != "reviewer-1" || watchString(t, reviewer, "kind") != "result" {
		t.Fatalf("追記resultが流れていません: %#v", reviewer)
	}
	exit := requireWatchEvent(t, events, "watch_exit")
	if watchString(t, exit, "status") != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("watch_exit = %#v", exit)
	}
	if watchEventIndex(events, "watch_exit") < watchEventIndex(events, "watch_start")+2 {
		t.Fatalf("終端eventが最終recordより先に出ています: %v", events)
	}
}

func TestWatchExitsWhenTaskIDSwitches(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)

	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, nil))
	}()
	time.Sleep(20 * time.Millisecond)
	if err := st.Write("task.id", "12345678-eeee-ffff-0000-111111111111"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchがtask切替後に終了しません")
	}

	events := parseWatchEvents(t, out.String())
	exit := requireWatchEvent(t, events, "watch_exit")
	if watchString(t, exit, "status") != "task-switched" || watchString(t, exit, "new_task_id") != "12345678-eeee-ffff-0000-111111111111" {
		t.Fatalf("task切替のwatch_exit = %#v", exit)
	}
}

func TestWatchExitsWhenEventLogRemoved(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)

	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, watchTestOptions(false, 5*time.Millisecond, nil))
	}()
	time.Sleep(20 * time.Millisecond)
	if err := os.Remove(st.TaskEventLogPath(taskID)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchがevent log削除後に終了しません")
	}
	events := parseWatchEvents(t, out.String())
	if watchString(t, requireWatchEvent(t, events, "event_log_status"), "status") != "removed" {
		t.Fatalf("log削除のstatus eventがありません: %v", events)
	}
	if watchEventIndex(events, "watch_exit") >= 0 {
		t.Fatalf("log削除終了にwatch_exitが出力されています: %v", events)
	}
}

func TestExecuteWatchReturnsAtNonActiveStatus(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "watchhash", RepoRoot: "/repo"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "result", Subtype: "success"},
	)

	cmd, err := ParseCommand([]string{"--watch"})
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- Execute(cmd, cfg, nil, out, &bytes.Buffer{})
	}()
	time.Sleep(20 * time.Millisecond)
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("production経路のwatchがnon-active遷移後に終了しません")
	}

	events := parseWatchEvents(t, out.String())
	start := requireWatchEvent(t, events, "watch_start")
	if watchString(t, start, "task_id") != taskID {
		t.Fatalf("watch_start = %#v", start)
	}
	if watchString(t, events[1], "kind") != "result" {
		t.Fatalf("保存済みrecordが流れていません: %#v", events[1])
	}
	exit := requireWatchEvent(t, events, "watch_exit")
	if watchString(t, exit, "status") != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("watch_exit = %#v", exit)
	}
	if st.ReadOr("task.status", "") != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("watchがtask.statusを変更しました: %s", st.ReadOr("task.status", ""))
	}
	if st.Exists("lock") {
		t.Fatal("watchがrepo lockを作成しました")
	}
}
