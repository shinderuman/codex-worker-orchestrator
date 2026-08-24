package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
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

// renderWatchLiveはwriteWatchLiveStatusでlive event 1行を出し、typed eventへ復元する。
func renderWatchLive(t *testing.T, st *state.StateStore, taskID string, tracker *watchToolTracker, now time.Time) watchLiveEvent {
	t.Helper()
	out := &bytes.Buffer{}
	if err := writeWatchLiveStatus(st, taskID, out, tracker, now); err != nil {
		t.Fatal(err)
	}
	return parseWatchLiveLine(t, out.String())
}

// parseWatchLiveLineはlive event 1行をtyped eventへparseする。
func parseWatchLiveLine(t *testing.T, line string) watchLiveEvent {
	t.Helper()
	var event watchLiveEvent
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event); err != nil {
		t.Fatalf("live eventがmachine JSONLではありません: %v: %q", err, line)
	}
	if event.Type != "live" {
		t.Fatalf("live eventのtype = %q", event.Type)
	}
	return event
}

// liveEventsFromStreamはwatch出力全体からlive eventだけを取り出す。
func liveEventsFromStream(t *testing.T, rendered string) []watchLiveEvent {
	t.Helper()
	var lives []watchLiveEvent
	for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		if !strings.Contains(line, `"type":"live"`) && !strings.Contains(line, `"type": "live"`) {
			continue
		}
		lives = append(lives, parseWatchLiveLine(t, line))
	}
	return lives
}

// requireModelIdleMSはlive eventのmodel_idle_msを検査する。
func requireModelIdleMS(t *testing.T, event watchLiveEvent, wantMS int64) {
	t.Helper()
	if event.ModelIdleMS == nil {
		t.Fatalf("model_idle_msがありません: %#v", event)
	}
	if *event.ModelIdleMS != wantMS {
		t.Fatalf("model_idle_ms = %d want %d", *event.ModelIdleMS, wantMS)
	}
}

// liveToolOfは実行中tool一覧から指定nameの要素を返す。
func liveToolOf(t *testing.T, event watchLiveEvent, name string) watchLiveTool {
	t.Helper()
	for _, tool := range event.Current {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("live eventのcurrentに%qがありません: %#v", name, event)
	return watchLiveTool{}
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
// command・purpose・経過が型付きlive eventへ出ることを検証する。
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
	lives := liveEventsFromStream(t, out.String())
	if len(lives) != 1 {
		t.Fatalf("live event = %d件: %s", len(lives), out.String())
	}
	live := lives[0]
	if live.TaskAgeMS == nil || *live.TaskAgeMS != int64((2*3600+50*60+46)*1000) {
		t.Fatalf("task_age_ms = %#v", live.TaskAgeMS)
	}
	if live.ModelIdleMS == nil || *live.ModelIdleMS != int64((2*3600+40*60+46)*1000) {
		t.Fatalf("model_idle_ms = %#v", live.ModelIdleMS)
	}
	bash := liveToolOf(t, live, "Bash")
	if bash.ElapsedMS != int64((2*3600+40*60+46)*1000) {
		t.Fatalf("Bash elapsed_ms = %d", bash.ElapsedMS)
	}
	if bash.Command != "sleep 295; grep -c 'GATE-START' /tmp/instr4_full.log" || bash.Purpose != "Check fourth run at gate section" {
		t.Fatalf("Bash詳細 = %#v", bash)
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
// いてもlive eventを出さないことを検証する。
func TestWatchPlainOutputUnchangedByVerboseData(t *testing.T) {
	st, _, _ := setupVerboseWatchTask(t)

	stop := make(chan struct{})
	close(stop)
	out := &bytes.Buffer{}
	if err := printWatch(st, out, watchTestOptions(false, time.Millisecond, stop)); err != nil {
		t.Fatal(err)
	}
	events := parseWatchEvents(t, out.String())
	if watchEventIndex(events, "live") >= 0 {
		t.Fatalf("--watch単体にlive eventが出ています: %v", events)
	}
}

// watchModelActivityResultLineはproducer/consumer統合testでcallを正常終端させる
// result event行。structured_outputはpacket.Resultのmachine JSONをそのまま埋め、
// 手書きescapeのJSON破損を構造的に排除する。
func watchModelActivityResultLine() string {
	structured := appPacketBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "done",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
	data, err := json.Marshal(map[string]any{
		"type":              "result",
		"subtype":           "success",
		"is_error":          false,
		"result":            structured,
		"structured_output": json.RawMessage(structured),
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}

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
	outPath := filepath.Join(dir, "out.log")
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", outPath); err != nil {
		runOut, _ := os.ReadFile(outPath)
		runErrText, _ := os.ReadFile(outPath + ".stderr")
		t.Fatalf("producer Run: %v out=%q stderr=%q", err, runOut, runErrText)
	}
}

// TestWatchVerboseModelIdleUsesLiveModelActivityはassistant activity → thinking_tokens →
// tool_progressの順で発生したstreamを本物のrunnerで摄取し、watchのMODEL_IDLE基準が
// event logへ保存されないthinking_tokens観測時刻(live snapshotのmodel activity専用時刻)
// になることを検証する。assistant基準なら+1分以上、tool_progress/result基準なら-1分以下に
// なる観測間隔で判定する。
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
		watchModelActivityResultLine(),
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

	requireModelIdleMS(t, renderWatchLive(t, st, taskID, tracker, snapshot.LastModelActivityAt.Add(150*time.Second)), 150000)
	requireModelIdleMS(t, renderWatchLive(t, st, taskID, tracker, snapshot.LastModelActivityAt.Add(240*time.Second)), 240000)
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

	live := renderWatchLive(t, st, taskID, tracker, base.Add(150*time.Second))
	requireModelIdleMS(t, live, 150000)
	if bash := liveToolOf(t, live, "Bash"); bash.Command != "sleep 295" {
		t.Fatalf("旧snapshotの詳細が落ちています: %#v", bash)
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

	requireModelIdleMS(t, renderWatchLive(t, st, taskID, tracker, base.Add(2*time.Minute)), 120000)
	fourMinutes := renderWatchLive(t, st, taskID, tracker, base.Add(4*time.Minute))
	requireModelIdleMS(t, fourMinutes, 240000)
	if bash := liveToolOf(t, fourMinutes, "Bash"); bash.ElapsedMS != 240000 || bash.Command != "sleep 295" {
		t.Fatalf("非model eventでtool観測が失われています: %#v", bash)
	}
}

// TestWatchVerboseToolLifecycleTransitionsはtool完了でcurrentから外れてlastへ遷移し、
// 経過時間が観測時刻とともに更新されること、未対応のresult eventで同一callのpendingが
// 除かれることを検証する。
func TestWatchVerboseToolLifecycleTransitions(t *testing.T) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}})

	if bash := liveToolOf(t, renderWatchLive(t, verboseRenderStore(t), "", tracker, base.Add(5*time.Minute)), "Bash"); bash.ElapsedMS != 300000 {
		t.Fatalf("実行中elapsed_ms = %d", bash.ElapsedMS)
	}
	if bash := liveToolOf(t, renderWatchLive(t, verboseRenderStore(t), "", tracker, base.Add(6*time.Minute)), "Bash"); bash.ElapsedMS != 360000 {
		t.Fatalf("elapsed更新 = %d", bash.ElapsedMS)
	}

	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "user", Timestamp: base.Add(10 * time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_1", DurationMS: 295100}}})
	completed := renderWatchLive(t, verboseRenderStore(t), "", tracker, base.Add(11*time.Minute))
	if len(completed.Current) != 0 {
		t.Fatalf("完了後のcurrent = %#v", completed.Current)
	}
	if completed.Last == nil || completed.Last.Name != "Bash" || completed.Last.DurationMS != 295100 {
		t.Fatalf("last = %#v", completed.Last)
	}

	tracker.observe(state.TaskEventRecord{CallID: "call-2", Kind: "assistant", Timestamp: base.Add(12 * time.Minute), Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_2"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-2", Kind: "result", Subtype: "success", Timestamp: base.Add(13 * time.Minute)})
	if abandoned := renderWatchLive(t, verboseRenderStore(t), "", tracker, base.Add(14*time.Minute)); len(abandoned.Current) != 0 {
		t.Fatalf("result event後のpending清除 = %#v", abandoned.Current)
	}
}

// TestWatchVerboseShortToolCompletionStaysOffCurrentは短いtoolの完了でlastが置き換わらず、
// currentだけが外れることを検証する。
func TestWatchVerboseShortToolCompletionStaysOffCurrent(t *testing.T) {
	base := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	tracker := newWatchToolTracker()
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Read", ToolID: "toolu_1"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "assistant", Timestamp: base, Blocks: []state.TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_2"}}})
	tracker.observe(state.TaskEventRecord{CallID: "call-1", Kind: "user", Timestamp: base.Add(300 * time.Millisecond), Blocks: []state.TaskBlockSummary{{Type: "tool_result", ToolID: "toolu_1", DurationMS: 300}}})

	live := renderWatchLive(t, verboseRenderStore(t), "", tracker, base.Add(time.Second))
	liveToolOf(t, live, "Bash")
	if len(live.Current) != 1 {
		t.Fatalf("短いtool完了後のcurrent = %#v", live.Current)
	}
	if live.Last != nil {
		t.Fatalf("短時間toolがlastへ出ています: %#v", live.Last)
	}
}

// TestWatchVerboseBackgroundWaitAndErrorはbackground待ちと直近tool errorをlive eventで
// 確認できることを検証する。待機中toolはcurrentとwait_task_idで停止と誤認しない。
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

	live := renderWatchLive(t, st, taskID, tracker, base.Add(2*time.Minute))
	wait := liveToolOf(t, live, "TaskOutput")
	if wait.ElapsedMS != 120000 || wait.WaitTaskID != "bash_7" {
		t.Fatalf("background待ちtool = %#v", wait)
	}
	if live.Last == nil || live.Last.Name != "Bash" || live.Last.DurationMS != 65000 {
		t.Fatalf("last = %#v", live.Last)
	}
	if live.ToolError == nil || live.ToolError.Name != "Bash" || live.ToolError.AgeMS != 60000 {
		t.Fatalf("tool_error = %#v", live.ToolError)
	}
}

// TestWatchVerboseTruncatesLongDetailAtDisplayは長いcommand・改行入りcommandのlive event
// 出力時切詰めを検証する。保存側(snapshot)の本文は変更しない。
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

	live := renderWatchLive(t, st, taskID, tracker, base.Add(time.Minute))
	command := liveToolOf(t, live, "Bash").Command
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
// と経過の観測を続けることを検証する。
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
	lives := liveEventsFromStream(t, out.String())
	if len(lives) == 0 {
		t.Fatalf("snapshot欠損でlive eventが落ちました: %s", out.String())
	}
	bash := liveToolOf(t, lives[0], "Bash")
	if bash.Command != "" || bash.Purpose != "" {
		t.Fatalf("snapshot欠損時に詳細fieldが出ています: %#v", bash)
	}
}

// TestWatchVerboseFollowReprintsOnToolCompletionはfollow中のtool完了でlive eventが
// current空へ更新されることを検証する。
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

	events := parseWatchEvents(t, out.String())
	lives := liveEventsFromStream(t, out.String())
	if len(lives) == 0 || len(lives[0].Current) != 1 || lives[0].Current[0].Name != "Bash" {
		t.Fatalf("実行中live eventがありません: %v", lives)
	}
	final := lives[len(lives)-1]
	if len(final.Current) != 0 || final.Last == nil || final.Last.Name != "Bash" || final.Last.DurationMS != 295100 {
		t.Fatalf("完了後のlive event = %#v", final)
	}
	if liveIndex := watchEventIndex(events, "live"); liveIndex < 0 || liveIndex > watchEventIndex(events, "watch_exit") {
		t.Fatalf("live eventが終端eventより後に出ています: %v", events)
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
	events := parseWatchEvents(t, out.String())
	if watchString(t, requireWatchEvent(t, events, "watch_start"), "event_log_status") != "none" {
		t.Fatalf("verbose watch出力 = %v", events)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--watch --verboseがstate dirを作成しました: %v", entries)
	}
}
