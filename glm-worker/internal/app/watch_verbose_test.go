package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// verboseTestClockは固定観測時刻を返すtest用clock。
func verboseTestClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// verboseRenderStoreはlive snapshotを持たない表示検証用store。
func verboseRenderStore(t *testing.T) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "watchhash",
		RepoRoot:  "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// setupVerboseWatchTaskはstats mirrorの開始時刻・event log・live snapshotを固定時刻で
// 用意し、watch対象storeと固定nowを返す。
func setupVerboseWatchTask(t *testing.T) (*state.StateStore, string, time.Time) {
	t.Helper()
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	st.InitializeTaskStats(taskID)
	st.UpdateTaskStats(func(stats *state.TaskStats) { stats.StartedAt = base })
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init", Timestamp: base},
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "assistant", Timestamp: base.Add(10 * time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1", Bytes: 88}}},
	)
	if err := st.WriteTaskLiveStatus(taskID, state.TaskLiveStatus{
		UpdatedAt:   base.Add(10 * time.Minute),
		LastEventAt: base.Add(10 * time.Minute),
		Tools: []state.TaskLiveTool{{
			ToolID:  "toolu_1",
			Command: "sleep 295; grep -c 'GATE-START' /tmp/instr4_full.log",
			Purpose: "Check fourth run at gate section",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return st, taskID, base
}

// TestParseCommandWatchVerboseは--watch --verboseだけを受け付け、他の引数と--watch単体を
// 従来どおり扱うことを検証する。
func TestParseCommandWatchVerbose(t *testing.T) {
	cmd, err := ParseCommand([]string{"--watch", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeWatch || !cmd.WatchVerbose {
		t.Fatalf("verbose watch command = %#v", cmd)
	}

	plain, err := ParseCommand([]string{"--watch"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Mode != ModeWatch || plain.WatchVerbose {
		t.Fatalf("watch command = %#v", plain)
	}
	for _, args := range [][]string{{"--watch", "--verbose", "extra"}, {"--watch", "extra"}, {"--watch", "-v"}} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("余分な引数が受け入れられています: %v", args)
		}
	}
}

// TestWatchVerboseRendersLiveToolStatusはverbose指定でtask年齢・model idle・実行中Bashの
// command・purpose・経過がLIVE行に出ることを検証する。
func TestWatchVerboseRendersLiveToolStatus(t *testing.T) {
	st, _, base := setupVerboseWatchTask(t)
	now := base.Add(2*time.Hour + 50*time.Minute + 46*time.Second)

	entriesBefore, err := os.ReadDir(st.Path("."))
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	opts := watchTestOptions(true, time.Millisecond, stop)
	opts.now = verboseTestClock(now)
	if err := printWatch(st, out, opts); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{
		"LIVE TASK_AGE 2:50:46",
		"LIVE MODEL_IDLE 2:40:46",
		"LIVE CURRENT Bash 2:40:46 elapsed",
		"LIVE COMMAND sleep 295; grep -c 'GATE-START' /tmp/instr4_full.log",
		"LIVE PURPOSE Check fourth run at gate section",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("verbose表示に%qがありません: %s", want, rendered)
		}
	}
	entriesAfter, err := os.ReadDir(st.Path("."))
	if err != nil {
		t.Fatal(err)
	}
	if len(entriesBefore) != len(entriesAfter) {
		t.Fatalf("verbose watchがstate dirを変更しました: %d -> %d", len(entriesBefore), len(entriesAfter))
	}
}

// TestWatchPlainOutputUnchangedByVerboseDataは--watch単体がlive snapshot・statsを読んで
// いてもLIVE行を出さないことを検証する。
func TestWatchPlainOutputUnchangedByVerboseData(t *testing.T) {
	st, _, _ := setupVerboseWatchTask(t)

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	if err := printWatch(st, out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "LIVE ") {
		t.Fatalf("--watch単体にLIVE行が出ています: %s", rendered)
	}
	if strings.Count(rendered, "\n") != 5 {
		t.Fatalf("--watch単体の表示行数が変わっています: %q", rendered)
	}
}

// watchModelActivityResultLineはproducer/consumer統合testでcallを正常終端させる
// result event行。structured_outputまで本物の形へ合わせる。
const watchModelActivityResultLine = `{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"}","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}}`

// runWatchModelActivityProducerは行間1秒のfake claude scriptで本物のClaudeRunnerを
// 1 call実行し、watch対象storeと同じstate dirへevent logとlive snapshotを書かせる。
func runWatchModelActivityProducer(t *testing.T, st *state.StateStore, lines ...string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n"
	for index, line := range lines {
		if index > 0 {
			script += "sleep 1\n"
		}
		script += fmt.Sprintf("printf '%%s\\n' '%s'\n", line)
	}
	commandPath := filepath.Join(dir, "fake-claude")
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := runner.NewClaudeRunner(config.AppConfig{
		RepoRoot:  dir,
		RepoShort: "abcdef123456",
		PromptDir: promptDir,
		ClaudeBin: commandPath,
	}, st)
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(dir, "out.log")); err != nil {
		t.Fatal(err)
	}
}

// TestWatchVerboseModelIdleUsesLiveModelActivityはassistant activity → thinking_tokens →
// tool_progressの順で発生したstreamを本物のrunnerで摄取し、watchのMODEL_IDLE基準が
// event logへ保存されないthinking_tokens観測時刻(live snapshotのmodel activity専用時刻)
// になることを検証する。assistant基準なら2:31以上、tool_progress/result基準なら
// 2:29以下になる時刻で判定する。
func TestWatchVerboseModelIdleUsesLiveModelActivity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	runWatchModelActivityProducer(t, st,
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"sleep 295"}}]}}`,
		`{"type":"system","subtype":"thinking_tokens"}`,
		`{"type":"system","subtype":"tool_progress"}`,
		watchModelActivityResultLine,
	)

	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	tracker := newWatchToolTracker()
	if _, err := drainTaskEvents(file, io.Discard, nil, tracker.observe); err != nil {
		t.Fatal(err)
	}
	snapshot, err := st.ReadTaskLiveStatus(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.LastModelActivityAt.After(tracker.lastModelActivityAt) {
		t.Fatalf("snapshotのmodel activity時刻(%v)がevent log側(%v)を追い抜いていません", snapshot.LastModelActivityAt, tracker.lastModelActivityAt)
	}
	if !snapshot.LastEventAt.After(snapshot.LastModelActivityAt) {
		t.Fatalf("generic時刻(%v)がmodel activity時刻(%v)より後であるべきです", snapshot.LastEventAt, snapshot.LastModelActivityAt)
	}

	now := snapshot.LastModelActivityAt.Add(150 * time.Second)
	out := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, out, tracker, now)
	rendered := out.String()
	if !strings.Contains(rendered, "LIVE MODEL_IDLE 2:30") {
		t.Fatalf("thinking_tokens基準のMODEL_IDLE = %q", rendered)
	}
	later := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, later, tracker, snapshot.LastModelActivityAt.Add(240*time.Second))
	if !strings.Contains(later.String(), "LIVE MODEL_IDLE 4:00") {
		t.Fatalf("tool_progress/result後もMODEL_IDLEが増え続けていません: %q", later.String())
	}
}

// TestWatchVerboseLegacySnapshotFallsBackToTrackerはlast_model_activity_atを持たない
// 旧live snapshotで、MODEL_IDLE基準がevent log側trackerのmodel activity時刻へ落ちる
// ことを検証する。migrationやgeneric last_event_atからの意味推定を挟まない安全側挙動。
func TestWatchVerboseLegacySnapshotFallsBackToTracker(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}})

	legacy := `{"updated_at":"` + base.Add(90*time.Second).Format(time.RFC3339Nano) + `","last_event_at":"` + base.Add(90*time.Second).Format(time.RFC3339Nano) + `","tools":[{"tool_id":"toolu_1","command":"sleep 295"}]}` + "\n"
	if err := os.MkdirAll(st.Path("events"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskLiveStatusPath(taskID), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, out, tracker, base.Add(150*time.Second))
	rendered := out.String()
	if !strings.Contains(rendered, "LIVE MODEL_IDLE 2:30") {
		t.Fatalf("旧snapshotでのMODEL_IDLE = %q(generic last_event_at基準なら1:00)", rendered)
	}
	if !strings.Contains(rendered, "LIVE COMMAND sleep 295") {
		t.Fatalf("旧snapshotの詳細行が落ちています: %q", rendered)
	}
}

// TestWatchVerboseModelIdleGrowsDuringToolProgressは長時間Bash中に30秒ごつの
// tool_progress等の非model eventが流れてもMODEL_IDLEが最後のassistant activityから
// 増え続けることを検証する。snapshotのlast_event_atが進んでいても基準にしない。
func TestWatchVerboseModelIdleGrowsDuringToolProgress(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}})
	for seconds := 30; seconds <= 120; seconds += 30 {
		tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "system", Subtype: "tool_progress", Timestamp: base.Add(time.Duration(seconds) * time.Second)})
	}
	if err := st.WriteTaskLiveStatus(taskID, state.TaskLiveStatus{
		UpdatedAt:   base.Add(120 * time.Second),
		LastEventAt: base.Add(120 * time.Second),
		Tools:       []state.TaskLiveTool{{ToolID: "toolu_1", Command: "sleep 295"}},
	}); err != nil {
		t.Fatal(err)
	}

	afterTwoMinutes := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, afterTwoMinutes, tracker, base.Add(2*time.Minute))
	if !strings.Contains(afterTwoMinutes.String(), "LIVE MODEL_IDLE 2:00") {
		t.Fatalf("tool_progress中のMODEL_IDLE = %q", afterTwoMinutes.String())
	}

	afterFourMinutes := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, afterFourMinutes, tracker, base.Add(4*time.Minute))
	rendered := afterFourMinutes.String()
	if !strings.Contains(rendered, "LIVE MODEL_IDLE 4:00") {
		t.Fatalf("MODEL_IDLEが増え続けていません: %q", rendered)
	}
	if !strings.Contains(rendered, "LIVE CURRENT Bash 4:00 elapsed") || !strings.Contains(rendered, "LIVE COMMAND sleep 295") {
		t.Fatalf("非model eventでtool表示が失われています: %q", rendered)
	}
}

// TestWatchVerboseToolLifecycleTransitionsはtool完了でCURRENTから外れてLASTへ遷移し、
// 経過時間が観測時刻とともに更新されること、未対応のresult eventで同一callのpendingが
// 除かれることを検証する。
func TestWatchVerboseToolLifecycleTransitions(t *testing.T) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}})

	running := &bytes.Buffer{}
	renderWatchLiveStatus(verboseRenderStore(t), "", running, tracker, base.Add(5*time.Minute))
	if !strings.Contains(running.String(), "LIVE CURRENT Bash 5:00 elapsed") {
		t.Fatalf("実行中表示 = %q", running.String())
	}

	updated := &bytes.Buffer{}
	renderWatchLiveStatus(verboseRenderStore(t), "", updated, tracker, base.Add(6*time.Minute))
	if !strings.Contains(updated.String(), "LIVE CURRENT Bash 6:00 elapsed") {
		t.Fatalf("elapsed更新表示 = %q", updated.String())
	}

	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "user", Timestamp: base.Add(10 * time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_1", DurationMS: 295100}}})
	completed := &bytes.Buffer{}
	renderWatchLiveStatus(verboseRenderStore(t), "", completed, tracker, base.Add(11*time.Minute))
	rendered := completed.String()
	if strings.Contains(rendered, "LIVE CURRENT Bash") || !strings.Contains(rendered, "LIVE CURRENT none") {
		t.Fatalf("完了後のCURRENT = %q", rendered)
	}
	if !strings.Contains(rendered, "LIVE LAST Bash completed 295.1s") {
		t.Fatalf("LAST表示 = %q", rendered)
	}

	tracker.observe(state.TaskEventRecord{CallID: "call-2", Kind: "assistant", Timestamp: base.Add(12 * time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_2"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-2", Kind: "result", Subtype: "success", Timestamp: base.Add(13 * time.Minute)})
	abandoned := &bytes.Buffer{}
	renderWatchLiveStatus(verboseRenderStore(t), "", abandoned, tracker, base.Add(14*time.Minute))
	if !strings.Contains(abandoned.String(), "LIVE CURRENT none") {
		t.Fatalf("result event後のpending清除 = %q", abandoned.String())
	}
}

// TestWatchVerboseShortToolCompletionStaysOffCurrentは短いtoolの完了でLAST表示が
// 置き換わらず、CURRENTだけが外れることを検証する。
func TestWatchVerboseShortToolCompletionStaysOffCurrent(t *testing.T) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Read", ToolID: "toolu_1"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_2"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "user", Timestamp: base.Add(300 * time.Millisecond), Blocks: []state.TaskBlockSummary{{Type: "tool_result", ToolID: "toolu_1", DurationMS: 300}}})

	out := &bytes.Buffer{}
	renderWatchLiveStatus(verboseRenderStore(t), "", out, tracker, base.Add(time.Second))
	rendered := out.String()
	if !strings.Contains(rendered, "LIVE CURRENT Bash") || strings.Contains(rendered, "LIVE CURRENT Read") {
		t.Fatalf("短いtool完了後のCURRENT = %q", rendered)
	}
	if strings.Contains(rendered, "LIVE LAST Read") {
		t.Fatalf("短時間toolがLASTへ出ています: %q", rendered)
	}
}

// TestWatchVerboseBackgroundWaitAndErrorはbackground待ちと直近tool errorをverbose表示で
// 確認できることを検証する。待機中toolはCURRENTとBACKGROUND_WAITで停止と誤認しない。
func TestWatchVerboseBackgroundWaitAndError(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "TaskOutput", ToolID: "toolu_w"}, {Type: "tool_use", Name: "Bash", ToolID: "toolu_e"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "user", Timestamp: base.Add(time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_e", IsError: true, DurationMS: 65000}}})
	if err := st.WriteTaskLiveStatus(taskID, state.TaskLiveStatus{
		UpdatedAt:   base,
		LastEventAt: base,
		Tools: []state.TaskLiveTool{{
			ToolID:     "toolu_w",
			WaitTaskID: "bash_7",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, out, tracker, base.Add(2*time.Minute))
	rendered := out.String()
	for _, want := range []string{
		"LIVE CURRENT TaskOutput 2:00 elapsed",
		"LIVE BACKGROUND_WAIT task=bash_7",
		"LIVE LAST Bash completed 65.0s",
		"LIVE TOOL_ERROR Bash 1:00 ago",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("verbose表示に%qがありません: %s", want, rendered)
		}
	}
}

// TestWatchVerboseTruncatesLongDetailAtDisplayは長いcommand・改行入りcommandの表示時
// 切詰めを検証する。保存側(snapshot)の本文は変更しない。
func TestWatchVerboseTruncatesLongDetailAtDisplay(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	longCommand := strings.Repeat("c", watchDetailMaxRunes*3) + "\nsecond line"
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}})
	if err := st.WriteTaskLiveStatus(taskID, state.TaskLiveStatus{
		UpdatedAt:   base,
		LastEventAt: base,
		Tools:       []state.TaskLiveTool{{ToolID: "toolu_1", Command: longCommand}},
	}); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	renderWatchLiveStatus(st, taskID, out, tracker, base.Add(time.Minute))
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	var commandLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "LIVE COMMAND ") {
			commandLines = append(commandLines, line)
		}
	}
	if len(commandLines) != 1 {
		t.Fatalf("COMMAND行 = %d件: %q", len(commandLines), out.String())
	}
	command := strings.TrimPrefix(commandLines[0], "LIVE COMMAND ")
	if !strings.HasSuffix(command, "...") || len([]rune(command)) > watchDetailMaxRunes+3 {
		t.Fatalf("表示切詰め = %d runes", len([]rune(command)))
	}

	status, err := st.ReadTaskLiveStatus(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Tools[0].Command != longCommand {
		t.Fatalf("snapshot本文が表示切詰めで変更されています: %q", status.Tools[0].Command)
	}
}

// TestWatchVerboseWithoutLiveSnapshotDegradesはlive snapshotが無い・壊れていてもtool種別
// と経過の表示を続けることを検証する。
func TestWatchVerboseWithoutLiveSnapshotDegrades(t *testing.T) {
	st, taskID, _ := setupVerboseWatchTask(t)
	if err := os.Remove(st.TaskLiveStatusPath(taskID)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	opts := watchTestOptions(true, time.Millisecond, stop)
	opts.now = verboseTestClock(now)
	if err := printWatch(st, out, opts); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "LIVE CURRENT Bash") {
		t.Fatalf("snapshot欠損でCURRENT表示が落ちました: %s", rendered)
	}
	if strings.Contains(rendered, "LIVE COMMAND") || strings.Contains(rendered, "LIVE PURPOSE") {
		t.Fatalf("snapshot欠損時に詳細行が出ています: %s", rendered)
	}
}

// TestWatchVerboseFollowReprintsOnToolCompletionはfollow中のtool完了でLIVE表示が
// CURRENT noneへ更新されることを検証する。
func TestWatchVerboseFollowReprintsOnToolCompletion(t *testing.T) {
	st, taskID, _ := setupVerboseWatchTask(t)
	base := time.Date(2026, 8, 23, 9, 10, 0, 0, time.UTC)

	out := &bytes.Buffer{}
	done := make(chan error, 1)
	opts := watchTestOptions(true, 5*time.Millisecond, nil)
	opts.statusInterval = 10 * time.Millisecond
	opts.changeInterval = time.Millisecond
	go func() {
		done <- printWatch(st, out, opts)
	}()
	time.Sleep(20 * time.Millisecond)
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "user", Timestamp: base.Add(time.Second), Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_1", DurationMS: 295100}}},
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
		t.Fatal("verbose watchがnon-active遷移後に終了しません")
	}

	rendered := out.String()
	if !strings.Contains(rendered, "LIVE CURRENT Bash") {
		t.Fatalf("実行中LIVE表示がありません: %s", rendered)
	}
	if !strings.Contains(rendered, "LIVE CURRENT none") || !strings.Contains(rendered, "LIVE LAST Bash completed 295.1s") {
		t.Fatalf("完了後のLIVE表示がありません: %s", rendered)
	}
	if strings.Index(rendered, "LIVE LAST Bash") > strings.Index(rendered, "WATCH_EXIT") {
		t.Fatalf("LIVE表示が終了行より後に出ています: %s", rendered)
	}
}

// TestExecuteWatchVerboseDoesNotCreateStateは--watch --verbose実行がstate dirを一切
// 作成・書換しないことを検証する。
func TestExecuteWatchVerboseDoesNotCreateState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "watchhash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--watch", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "EVENT_LOG: none") {
		t.Fatalf("verbose watch出力 = %q", out.String())
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--watch --verboseがstate dirを作成しました: %v", entries)
	}
}
