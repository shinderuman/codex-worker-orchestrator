package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type fakeStep struct {
	structured string
	output     string
	runErr     error
}

type fakeRunner struct {
	steps     []fakeStep
	probeErrs []error
	prompts   []string
	models    []string
	probes    []string
}

func (r *fakeRunner) Run(
	_ state.SessionRole,
	_ string,
	model string,
	_ bool,
	_ string,
	prompt string,
	outputPath string,
) (runner.RunResult, error) {
	r.prompts = append(r.prompts, prompt)
	r.models = append(r.models, model)
	index := len(r.prompts) - 1
	step := r.steps[index]
	if step.output != "" {
		if err := os.WriteFile(outputPath, []byte(step.output), 0o600); err != nil {
			return runner.RunResult{}, err
		}
	}
	structured := json.RawMessage(step.structured)
	return runner.RunResult{SessionID: "test-session", StructuredOutput: structured, Response: string(structured)}, step.runErr
}

func (r *fakeRunner) Probe(model string) (runner.ProbeResult, error) {
	r.probes = append(r.probes, model)
	index := len(r.probes) - 1
	var err error
	if index < len(r.probeErrs) {
		err = r.probeErrs[index]
	}
	return runner.ProbeResult{
		Response: runner.ProbeSentinel,
		Usage:    runner.TokenUsage{InputTokens: 1, OutputTokens: 1},
	}, err
}

func (r *fakeRunner) factory() RunnerFactory {
	return func(_ config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner { return r }
}

func appPacketBody(result packet.Result) string {
	data, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func implementedPacketApp(summary string) string {
	return appPacketBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             summary,
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func passPacketApp() string {
	return appPacketBody(packet.Result{
		Status:              packet.StatusPass,
		Risk:                packet.RiskLow,
		Summary:             "pass",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "none",
		ResidualRisk:        "none",
		Targets:             []string{"final diff"},
	})
}

func newAppConfig(t *testing.T) config.AppConfig {
	t.Helper()
	return config.AppConfig{
		StateBase:             t.TempDir(),
		RepoHash:              "apphash",
		RepoRoot:              initGitRepo(t),
		RepoShort:             "appshort1234",
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		CodexConfigDir:        t.TempDir(),
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

func TestExecuteStatusReportsEmptyState(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	if err := Execute(Command{Mode: ModeStatus}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	output := executeStatusOutput(t, cfg)
	if output.TaskID != nil || output.ArtifactDir != nil {
		t.Fatalf("空状態のstatus出力 = %#v: %q", output, out.String())
	}
	if output.PendingDecision {
		t.Fatalf("空状態のpending_decision = true: %q", out.String())
	}
}

func TestExecuteStatsReportsEmptyState(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	if err := Execute(Command{Mode: ModeStats}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output statsOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("stats出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if output.Tasks != 0 || output.ModelCalls != 0 {
		t.Fatalf("空状態のstats出力 = %#v: %q", output, out.String())
	}
	if len(output.ModelCallsByAlias) != 0 || len(output.RateLimitsByAlias) != 0 {
		t.Fatalf("空状態のmodel別stats出力 = %#v: %q", output, out.String())
	}
	if output.TelemetryDir == "" {
		t.Fatalf("telemetry保存先がありません: %q", out.String())
	}
	if output.CurrentTask.ID != nil || output.CurrentTask.ArtifactDir != nil {
		t.Fatalf("空状態のcurrent_task = %#v: %q", output.CurrentTask, out.String())
	}
}

func TestPrintStatsAggregatesAndSortsModelAliases(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	st.RecordModelCall(state.ReviewerRole, "haiku")
	st.RecordModelCall(state.ReviewerRole, "sonnet")
	st.RecordModelDuration("sonnet", 2*time.Second)
	st.RecordRateLimit("opus")
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:     st.ReadOr("task.id", "unknown"),
		CallType:   state.CallTypeTask,
		ModelAlias: "opus",
		TopLevelUsage: state.TokenUsage{
			InputTokens:          1,
			CacheReadInputTokens: 2,
			OutputTokens:         3,
		},
		ResolvedModelUsage: map[string]state.ResolvedModelUsage{
			"glm-5.3": {InputTokens: 10, CacheCreationInputTokens: 20, CacheReadInputTokens: 30, OutputTokens: 40},
			"glm-4.7": {InputTokens: 5, CacheReadInputTokens: 7, OutputTokens: 8},
		},
		TopLevelTurns: 2,
	})

	output := executeStatsOutput(t, st)
	if len(output.ModelCallsByAlias) != 3 || output.ModelCallsByAlias["haiku"] != 1 || output.ModelCallsByAlias["opus"] != 1 || output.ModelCallsByAlias["sonnet"] != 1 {
		t.Fatalf("model別stats = %#v", output.ModelCallsByAlias)
	}
	if len(output.RateLimitsByAlias) != 1 || output.RateLimitsByAlias["opus"] != 1 {
		t.Fatalf("model別rate limit = %#v", output.RateLimitsByAlias)
	}
	if len(output.ModelDurationMSByAlias) != 1 || output.ModelDurationMSByAlias["sonnet"] != 2000 {
		t.Fatalf("model別実行時間 = %#v", output.ModelDurationMSByAlias)
	}
	if output.InputTokensByAlias["opus"] != 15 ||
		output.CacheCreationInputTokensByAlias["opus"] != 20 ||
		output.CacheReadInputTokensByAlias["opus"] != 37 ||
		output.TotalPromptTokensByAlias["opus"] != 72 ||
		output.OutputTokensByAlias["opus"] != 48 ||
		output.TopLevelTurnsByAlias["opus"] != 2 {
		t.Fatalf("token stats = %#v", output)
	}
	if len(output.CallTreesByResolvedModel) != 2 || output.CallTreesByResolvedModel["glm-4.7"] != 1 || output.CallTreesByResolvedModel["glm-5.3"] != 1 {
		t.Fatalf("resolved model別call trees = %#v", output.CallTreesByResolvedModel)
	}
	if output.InputTokensByResolvedModel["glm-5.3"] != 10 || output.InputTokensByResolvedModel["glm-4.7"] != 5 ||
		output.CacheCreationInputTokensByResolvedModel["glm-5.3"] != 20 ||
		output.CacheReadInputTokensByResolvedModel["glm-5.3"] != 30 || output.CacheReadInputTokensByResolvedModel["glm-4.7"] != 7 ||
		output.OutputTokensByResolvedModel["glm-5.3"] != 40 || output.OutputTokensByResolvedModel["glm-4.7"] != 8 {
		t.Fatalf("resolved model別token stats = %#v", output)
	}
}

func TestExecuteResetClearsTask(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/999-stale.md"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeReset}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var reset map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &reset); err != nil {
		t.Fatalf("reset出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if reset["status"] != "reset" {
		t.Fatalf("RESET出力がありません: %q", out.String())
	}
	if st.Exists("task.id") {
		t.Fatal("reset後もtask.idが残っています")
	}
	if st.Exists("active-task") {
		t.Fatal("reset後もactive-taskが残っています")
	}
}

func TestExecuteResumeRejectsNonRateLimited(t *testing.T) {
	cfg := newAppConfig(t)
	r := &fakeRunner{}

	err := Execute(Command{Mode: ModeResume}, cfg, r.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "resumable task is not available") {
		t.Fatalf("resumableでない--resumeを拒否する必要があります: %v", err)
	}
}

func TestExecuteNewTaskReachesPass(t *testing.T) {
	cfg := newAppConfig(t)
	r := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: passPacketApp()},
	}}

	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteAcquiresAndReleasesLock(t *testing.T) {
	cfg := newAppConfig(t)

	first := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, first.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Exists("lock") {
		t.Fatal("lockファイルが作成されていません")
	}
	if accept := executeAccept(t, cfg); !accept.Accepted {
		t.Fatal("lock解放後の次task開始前にparent reviewを解決できませんでした")
	}

	second := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, second.factory(), io.Discard, io.Discard); err != nil {
		t.Fatalf("前回実行のロック解放後も2回目の実行が失敗しました: %v", err)
	}
}

func TestExecutePropagatesWorkerFailure(t *testing.T) {
	cfg := newAppConfig(t)
	r := &fakeRunner{steps: []fakeStep{{runErr: errors.New("boom")}}}

	err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
	var workerErr *workflow.WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("worker失敗をtyped errorで伝播する必要があります: %v", err)
	}
	if workerErr.Message != "boom" {
		t.Fatalf("worker失敗のmessage = %q", workerErr.Message)
	}
}

func TestRunUsesInjectedDependencies(t *testing.T) {
	cfg := newAppConfig(t)
	r := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: passPacketApp()},
	}}
	var out bytes.Buffer

	err := run(
		[]string{"request"},
		func() (config.AppConfig, error) { return cfg, nil },
		r.factory(),
		strings.NewReader(""),
		&out,
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status":"PASS"`) {
		t.Fatalf("packetが指定stdoutへ出力されていません: %q", out.String())
	}
}

func TestRunStopsWhenConfigLoadFails(t *testing.T) {
	want := errors.New("config failure")
	err := run(
		[]string{"request"},
		func() (config.AppConfig, error) { return config.AppConfig{}, want },
		nil,
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
	if !errors.Is(err, want) {
		t.Fatalf("config error = %v", err)
	}
}

func TestExecuteVerifyAutoResumeFailsWhenTOMLMissing(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	err := Execute(Command{
		Mode: ModeVerifyAutoResume,
		Verify: VerifyArgs{
			Key:      "glm-worker-resume-nonexist-00000000",
			RFC3339:  "2026-08-12T20:01:20+09:00",
			ThreadID: "019f88f8-0e70-7d53-a2a3-f0c61666827c",
		},
	}, cfg, nil, &out, io.Discard)

	if err == nil {
		t.Fatal("missing TOML should return error")
	}
	var verification *VerificationError
	if !errors.As(err, &verification) || verification.Outcome != autoresume.Fail {
		t.Fatalf("verification fail typed errorを期待: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("失敗時のstdoutは空のまま: %q", out.String())
	}
	var errOut bytes.Buffer
	if err := WriteProcessError(&errOut, err); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("process errorがJSON 1行として読めません: %v: %q", err, errOut.String())
	}
	if envelope.Error.Kind != "verification_failed" {
		t.Fatalf("process error kind = %q: %s", envelope.Error.Kind, errOut.String())
	}
}

func TestExecuteVerifyAutoResumePassesWithValidTOMLAndDB(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}

	cfg := newAppConfig(t)
	key := "glm-worker-resume-appshort1234-abcd1234"
	thread := "019f88f8-0e70-7d53-a2a3-f0c61666827c"
	rfc3339 := "2026-08-12T20:01:20+09:00"

	automationsDir := cfg.CodexConfigDir + "/automations/" + key
	if err := os.MkdirAll(automationsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := `version = 1
id = "` + key + `"
kind = "heartbeat"
name = "` + key + `"
prompt = "resume"
status = "ACTIVE"
rrule = "DTSTART:20260812T110120\nRRULE:FREQ=DAILY;COUNT=1"
target_thread_id = "` + thread + `"
created_at = 1
`
	if err := os.WriteFile(automationsDir+"/automation.toml", []byte(tomlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	dbDir := cfg.CodexConfigDir + "/sqlite"
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := dbDir + "/codex-dev.db"
	schema := `CREATE TABLE automations (id TEXT PRIMARY KEY, name TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ACTIVE', next_run_at INTEGER, last_run_at INTEGER, cwds TEXT NOT NULL DEFAULT '[]', rrule TEXT NOT NULL, model TEXT, reasoning_effort TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, target_type TEXT, project_id TEXT);`
	if err := exec.Command("sqlite3", dbPath, schema).Run(); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	expectedMS := time.Date(2026, 8, 12, 11, 1, 20, 0, time.UTC).UnixMilli()
	insert := `INSERT INTO automations (id, name, prompt, status, next_run_at, cwds, rrule, created_at, updated_at) VALUES ('` + key + `', '` + key + `', 'p', 'ACTIVE', ` + fmt.Sprintf("%d", expectedMS) + `, '[]', 'DTSTART:20260812T110120' || char(10) || 'RRULE:FREQ=DAILY;COUNT=1', 1, 1);`
	if err := exec.Command("sqlite3", dbPath, insert).Run(); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var out bytes.Buffer
	err := Execute(Command{
		Mode: ModeVerifyAutoResume,
		Verify: VerifyArgs{
			Key:      key,
			RFC3339:  rfc3339,
			ThreadID: thread,
		},
	}, cfg, nil, &out, io.Discard)

	if err != nil {
		t.Fatalf("expected pass, got error: %v output=%s", err, out.String())
	}
	var output verifyAutoResumeOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("成功出力がmachine JSON 1行として読めません: %v: %q", err, out.String())
	}
	if output.AutomationKey != key || output.TargetThread != thread ||
		output.ExpectedAtUTC != "2026-08-12T11:01:20Z" || output.TOMLDTStart != "20260812T110120" ||
		output.DBNextRunAtUTC != "2026-08-12T11:01:20Z" {
		t.Fatalf("verify output = %+v", output)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("出力はJSON 1行だけ: %q", out.String())
	}
}

func TestExecuteCheckWakeCoalesceCoalescesActiveWake(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}

	cfg := newAppConfig(t)
	parentThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	wakeID := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"

	automationsDir := cfg.CodexConfigDir + "/automations/" + wakeID
	if err := os.MkdirAll(automationsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlContent := "version = 1\n" +
		"id = \"" + wakeID + "\"\n" +
		"kind = \"heartbeat\"\n" +
		"name = \"" + wakeID + "\"\n" +
		"prompt = \"親実装task " + parentThread + "へ固定文「作業を続けろ」を1回送信する\"\n" +
		"status = \"ACTIVE\"\n" +
		"rrule = \"DTSTART:20260826T152059\\nRRULE:FREQ=DAILY;COUNT=1\"\n" +
		"target_thread_id = \"01a03a9e-10a0-7f11-801c-f04e5dbd5490\"\n" +
		"created_at = 1\n"
	if err := os.WriteFile(automationsDir+"/automation.toml", []byte(tomlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	dbDir := cfg.CodexConfigDir + "/sqlite"
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := dbDir + "/codex-dev.db"
	schema := `CREATE TABLE automations (id TEXT PRIMARY KEY, name TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ACTIVE', next_run_at INTEGER, last_run_at INTEGER, cwds TEXT NOT NULL DEFAULT '[]', rrule TEXT NOT NULL, model TEXT, reasoning_effort TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, target_type TEXT, project_id TEXT);`
	if err := exec.Command("sqlite3", dbPath, schema).Run(); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	nextRun := time.Date(2026, 8, 26, 15, 20, 59, 0, time.UTC).UnixMilli()
	insert := `INSERT INTO automations (id, name, prompt, status, next_run_at, cwds, rrule, created_at, updated_at) VALUES ('` + wakeID + `', '` + wakeID + `', 'p', 'ACTIVE', ` + fmt.Sprintf("%d", nextRun) + `, '[]', 'DTSTART:20260826T152059' || char(10) || 'RRULE:FREQ=DAILY;COUNT=1', 1, 1);`
	if err := exec.Command("sqlite3", dbPath, insert).Run(); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var out bytes.Buffer
	err := Execute(Command{
		Mode: ModeCheckWakeCoalesce,
		Coalesce: CoalesceArgs{
			ParentThreadID:  parentThread,
			ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		},
	}, cfg, nil, &out, io.Discard)
	if err != nil {
		t.Fatalf("expected coalesce, got error: %v output=%s", err, out.String())
	}
	var output checkWakeCoalesceOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("成功出力がmachine JSON 1行として読めません: %v: %q", err, out.String())
	}
	if output.Decision != "coalesce" || output.Reason != "" ||
		output.WakeAutomationID != wakeID || output.WakeThread != "01a03a9e-10a0-7f11-801c-f04e5dbd5490" ||
		output.WakeNextRunUTC != "2026-08-26T15:20:59Z" || output.AddedWaitSeconds != 184 ||
		output.ParentThread != parentThread || output.ResumeAtUTC != "2026-08-26T15:17:55Z" {
		t.Fatalf("coalesce output = %+v", output)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("出力はJSON 1行だけ: %q", out.String())
	}
}

func TestExecuteCheckWakeCoalesceCreatesGLMWakeWithoutWake(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	err := Execute(Command{
		Mode: ModeCheckWakeCoalesce,
		Coalesce: CoalesceArgs{
			ParentThreadID:  "01a0244a-4ee4-7e71-b2e1-dec3bdda2120",
			ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		},
	}, cfg, nil, &out, io.Discard)
	if err != nil {
		t.Fatalf("expected create_glm_wake, got error: %v output=%s", err, out.String())
	}
	var output checkWakeCoalesceOutput
	if err := json.Unmarshal(out.Bytes(), &output); err != nil {
		t.Fatalf("成功出力がmachine JSON 1行として読めません: %v: %q", err, out.String())
	}
	if output.Decision != "create_glm_wake" || output.Reason != "no codex wake automation targets the parent thread" {
		t.Fatalf("create output = %+v", output)
	}
}

func TestExecuteCheckWakeCoalesceRejectsInvalidResumeTime(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	err := Execute(Command{
		Mode: ModeCheckWakeCoalesce,
		Coalesce: CoalesceArgs{
			ParentThreadID:  "01a0244a-4ee4-7e71-b2e1-dec3bdda2120",
			ResumeAtRFC3339: "2026-08-26 15:17:55",
		},
	}, cfg, nil, &out, io.Discard)
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("usage errorを期待: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("失敗時のstdoutは空のまま: %q", out.String())
	}
	var errOut bytes.Buffer
	if err := WriteProcessError(&errOut, err); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("process errorがJSON 1行として読めません: %v: %q", err, errOut.String())
	}
	if envelope.Error.Kind != "usage" {
		t.Fatalf("process error kind = %q: %s", envelope.Error.Kind, errOut.String())
	}
}

func TestInstructionMutationGuardRecoveryCLIAcceptance(t *testing.T) {
	cfg := newAppConfig(t)
	seedGitCommit(t, cfg.RepoRoot)
	decisionWait := &fakeRunner{steps: []fakeStep{
		{structured: needsSolDecisionPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, decisionWait.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	waiting := executeStatusOutput(t, cfg)
	statusString(t, "task_status", waiting.TaskStatus, string(state.TaskStatusWaitingDecision))
	if !waiting.PendingDecision {
		t.Fatal("decision待ち状態でpending_decisionがfalse")
	}

	guardRunner := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("decision done"), runErr: &runner.InstructionSurfaceGuardError{
			Stage:        "after-call-mutation",
			ChangedPaths: []string{"codex/AGENTS.md"},
			Restored:     true,
		}},
	}}
	err := Execute(Command{Mode: ModeDecision, Payload: "A案で進める"}, cfg, guardRunner.factory(), io.Discard, io.Discard)
	var stopped *workflow.GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("GuardRecoverableErrorを期待: %v", err)
	}

	stoppedStatus := executeStatusOutput(t, cfg)
	statusString(t, "task_status", stoppedStatus.TaskStatus, string(state.TaskStatusGuardRecoverable))
	if stoppedStatus.PendingDecision {
		t.Fatal("guard停止後もpending_decisionがtrue")
	}
	if !stoppedStatus.ResumeAvailable {
		t.Fatal("guard停止後にresume_availableがfalse")
	}

	assertGuardRecoveryCommandRejected(t, cfg, Command{Mode: ModeDecision, Payload: "再送"}, "no pending Sol decision")
	assertGuardRecoveryCommandRejected(t, cfg, Command{Mode: ModeFix, Payload: "fix"}, "--fix is only available after NEEDS_SOL_REVIEW")
	assertGuardRecoveryCommandRejected(t, cfg, Command{Mode: ModeNewTask, Payload: "replacement"}, "recoverable guard failure")

	resumeRunner := &fakeRunner{steps: []fakeStep{
		{structured: passPacketApp()},
		{structured: needsSolReviewPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeResume}, cfg, resumeRunner.factory(), io.Discard, io.Discard); err != nil {
		t.Fatalf("same-task resumeが失敗: %v", err)
	}
	final := executeStatusOutput(t, cfg)
	statusString(t, "task_status", final.TaskStatus, string(state.TaskStatusWaitingSolReview))
	if final.ResumeAvailable || final.PendingDecision {
		t.Fatalf("terminal状態 = resume:%v pending:%v", final.ResumeAvailable, final.PendingDecision)
	}
	if len(resumeRunner.prompts) != 2 {
		t.Fatalf("保存済みworker resultを再利用し独立reviewとrisk floor再出力だけで閉じるべき: prompts=%d", len(resumeRunner.prompts))
	}
}

func seedGitCommit(t *testing.T, repoRoot string) {
	t.Helper()
	command := exec.Command("git", "-C", repoRoot,
		"-c", "user.email=guard-recovery@example.invalid",
		"-c", "user.name=guard recovery test",
		"commit", "-q", "--allow-empty", "-m", "seed")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
}

func assertGuardRecoveryCommandRejected(t *testing.T, cfg config.AppConfig, cmd Command, wantIn string) {
	t.Helper()
	rejected := &fakeRunner{}
	err := Execute(cmd, cfg, rejected.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), wantIn) {
		t.Fatalf("mode %dを拒否する必要があります(%q): %v", cmd.Mode, wantIn, err)
	}
	if len(rejected.prompts) != 0 {
		t.Fatalf("拒否されたcommandがmodel呼出を実行しました: %d", len(rejected.prompts))
	}
}
