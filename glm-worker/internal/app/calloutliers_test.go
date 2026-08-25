package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// executeCallOutliersは--call-outliers相当の実行出力1行をraw JSONへdecodeする。
func executeCallOutliers(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printCallOutliers(st, &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

// recordCallOutliersFixtureは1 task分のtelemetry記録を本番writer経由で追記する。
func recordCallOutliersFixture(t *testing.T, st *state.StateStore, taskID string, base time.Time) {
	t.Helper()
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-a",
		Role: state.WorkerRole, ModelAlias: "opus", Phase: "worker-new",
		StartedAt: base, CompletedAt: base.Add(time.Minute),
		Outcome: "success", WallDurationMS: 60000, TopLevelTurns: 120,
		Prompt: "raw-prompt-must-not-leak", Response: "raw-response-must-not-leak",
	})
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-a",
		Role: state.WorkerRole, ModelAlias: "opus", Phase: "worker-new", Resumed: true,
		StartedAt: base.Add(time.Hour), CompletedAt: base.Add(time.Hour).Add(2 * time.Minute),
		Outcome: "success", WallDurationMS: 120000, TopLevelTurns: 320,
	})
}

// TestExecuteCallOutliersAggregatesSavedTelemetryは保存済みtelemetryだけから全task横断の
// 分布・増幅・outlier報告を出し、prompt/response本文を出さないことを検証する。
func TestExecuteCallOutliersAggregatesSavedTelemetry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordCallOutliersFixture(t, st, taskA, base)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskC := "33333333-3333-4333-8333-333333333333"
	recordCallOutliersFixture(t, st, taskC, base.Add(3*time.Hour))

	decoded := executeCallOutliers(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "ok" {
		t.Fatalf("telemetry status = %#v", telemetry)
	}
	if telemetry["files"].(float64) != 2 {
		t.Fatalf("telemetry files = %#v", telemetry)
	}
	if dir, _ := telemetry["dir"].(string); dir != st.Path("telemetry") {
		t.Fatalf("telemetry dir = %#v", telemetry)
	}

	report, _ := decoded["report"].(map[string]any)
	if report["percentile_method"] != "linear" {
		t.Fatalf("percentile_method = %#v", report["percentile_method"])
	}
	records, _ := report["records"].(map[string]any)
	if records["read"].(float64) != 4 || records["task_calls"].(float64) != 4 {
		t.Fatalf("records = %#v", records)
	}
	distributions, _ := report["distributions"].([]any)
	if len(distributions) != 2 {
		t.Fatalf("distributions = %#v", distributions)
	}
	current, _ := distributions[0].(map[string]any)
	if current["phase"] != "worker-new" || current["role"] != "worker" || current["resumed"] != false {
		t.Fatalf("current group = %#v", current)
	}
	turns, _ := current["turns"].(map[string]any)
	if turns["median"].(float64) != 120 || turns["p95"].(float64) != 120 || turns["total"].(float64) != 240 {
		t.Fatalf("current turns = %#v", turns)
	}
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %#v", tasks)
	}
	top, _ := tasks[0].(map[string]any)
	if top["turns_total"].(float64) != 440 {
		t.Fatalf("tasks並び = %#v", tasks)
	}
	initial, _ := top["initial"].(map[string]any)
	if initial["turns"].(float64) != 120 || initial["outcome"] != "success" {
		t.Fatalf("task initial = %#v", initial)
	}
	if multiplier, _ := top["turns_x_initial"].(float64); multiplier != 3.67 {
		t.Fatalf("turns_x_initial = %#v", top["turns_x_initial"])
	}
	sessions, _ := report["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
	session, _ := sessions[0].(map[string]any)
	if session["tasks"].(float64) != 2 || session["calls"].(float64) != 4 || session["turns_total"].(float64) != 880 {
		t.Fatalf("session = %#v", session)
	}
	outlierCalls, _ := report["outlier_calls"].([]any)
	outlierTasks, _ := report["outlier_tasks"].([]any)
	if len(outlierCalls) != 0 || len(outlierTasks) != 0 {
		t.Fatalf("母数不足なのにoutlier = %#v / %#v", outlierCalls, outlierTasks)
	}

	var rendered bytes.Buffer
	if err := printCallOutliers(st, &rendered); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"raw-prompt-must-not-leak", "raw-response-must-not-leak", "\"prompt\"", "\"response\""} {
		if strings.Contains(rendered.String(), secret) {
			t.Fatalf("出力にprompt/response本文が含まれています: %q in %s", secret, rendered.String())
		}
	}
}

// TestCallOutliersEmptyTelemetryDirIsNoneは空のtelemetry dir(読取可能・fileなし)を
// status noneで正常終了することを検証する。
func TestCallOutliersEmptyTelemetryDirIsNone(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}

	decoded := executeCallOutliers(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "none" || telemetry["files"].(float64) != 0 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
}

// TestCallOutliersDirReadErrorIsProcessErrorはtelemetry dirの読取失敗(不在以外)を
// 正常noneへ偽装せずerrorで返し、process境界のinternal error契約へ乗ることを検証する。
// dir pathへ通常fileを置くことでENOTDIRを決定論的に起こす。
func TestCallOutliersDirReadErrorIsProcessError(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(filepath.Dir(st.Path("telemetry")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("telemetry"), []byte("not-a-dir\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := Execute(Command{Mode: ModeCallOutliers}, cfg, nil, &out, io.Discard)

	if err == nil {
		t.Fatalf("dir読取失敗が正常終了しました: %s", out.String())
	}
	if out.Len() != 0 {
		t.Fatalf("失敗時にstdoutへ出力があります: %q", out.String())
	}
	envelope, _ := writeProcessErrorJSON(t, err)
	if envelope.Error.Kind != "internal" || envelope.Error.Message == "" {
		t.Fatalf("process error = %#v", envelope.Error)
	}
}

// TestExecuteCallOutliersEmptyStateはtelemetryがまだないstateを正常終了し、state dirを
// 作成しないことを検証する。
func TestExecuteCallOutliersEmptyState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "calloutliershash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--call-outliers"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeCallOutliers {
		t.Fatalf("command = %+v", cmd)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "none" || telemetry["files"].(float64) != 0 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	report, _ := decoded["report"].(map[string]any)
	for _, key := range []string{"distributions", "models", "sessions", "tasks", "outlier_calls", "outlier_tasks"} {
		value, ok := report[key].([]any)
		if !ok || value == nil {
			t.Fatalf("reportの%qが空配列ではありません: %#v", key, report[key])
		}
		if len(value) != 0 {
			t.Fatalf("reportの%qが空ではありません: %#v", key, value)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--call-outliersがstate dirを作成しました: %v", entries)
	}
}

// TestCallOutliersPartialOnUnreadableTelemetryは破損telemetry fileがあっても他taskの
// 集計を出し、失敗をunreadable_tasksへ残すことを検証する。
func TestCallOutliersPartialOnUnreadableTelemetry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordCallOutliersFixture(t, st, taskA, time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	broken := "44444444-4444-4444-8444-444444444444"
	path := st.ModelCallLogPath(broken)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"version\":3,\"call_type\":\"task\",\"not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	decoded := executeCallOutliers(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "partial" {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	unreadable, _ := telemetry["unreadable_tasks"].([]any)
	if len(unreadable) != 1 {
		t.Fatalf("unreadable_tasks = %#v", unreadable)
	}
	entry, _ := unreadable[0].(map[string]any)
	if entry["task_id"] != broken || entry["error"] == "" {
		t.Fatalf("unreadable entry = %#v", entry)
	}
	if telemetry["files"].(float64) != 1 {
		t.Fatalf("files = %#v", telemetry)
	}
	report, _ := decoded["report"].(map[string]any)
	records, _ := report["records"].(map[string]any)
	if records["read"].(float64) != 2 {
		t.Fatalf("records = %#v", records)
	}
}

// TestCallOutliersIgnoresNonTaskTelemetryFilesはtask ID生成形式に合わないfile名を
// 読まずignored_filesへ出すことを検証する。
func TestCallOutliersIgnoresNonTaskTelemetryFiles(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordCallOutliersFixture(t, st, taskA, time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"evil.jsonl", "notes.jsonl"} {
		if err := os.WriteFile(filepath.Join(st.Path("telemetry"), name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	decoded := executeCallOutliers(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "ok" {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	ignored, _ := telemetry["ignored_files"].([]any)
	if len(ignored) != 2 {
		t.Fatalf("ignored_files = %#v", ignored)
	}
	ignoredNames := map[string]bool{}
	for _, entry := range ignored {
		name, _ := entry.(string)
		ignoredNames[name] = true
	}
	if !ignoredNames["evil.jsonl"] || !ignoredNames["notes.jsonl"] {
		t.Fatalf("ignored_files = %#v", ignored)
	}
	if telemetry["files"].(float64) != 1 {
		t.Fatalf("files = %#v", telemetry)
	}
}

func TestParseCommandCallOutliers(t *testing.T) {
	cmd, err := ParseCommand([]string{"--call-outliers"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeCallOutliers {
		t.Fatalf("command = %+v", cmd)
	}
	if _, err := ParseCommand([]string{"--call-outliers", "extra"}); err == nil {
		t.Fatal("余分な引数が受け入れられています")
	}
}
