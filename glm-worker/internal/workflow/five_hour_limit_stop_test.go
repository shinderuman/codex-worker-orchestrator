package workflow

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// processModelRunnerは本物のClaudeRunnerをworkflowへ接続するadapter。呼出回数だけ
// 数え、5h limit早期停止後の追加retry 0回をproduction構成で検証する。
type processModelRunner struct {
	inner *runner.ClaudeRunner
	calls int
}

func (a *processModelRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (runner.RunResult, error) {
	a.calls++
	return a.inner.Run(role, phase, model, readOnly, effort, prompt, outputPath)
}

func (a *processModelRunner) Probe(string) (runner.ProbeResult, error) {
	return runner.ProbeResult{}, errors.New("probeは本scenarioでは呼ばれない")
}

// TestFiveHourLimitEarlyStopSavesRateLimitedOnceはexact 5h limit signalをstderrまたは
// JSON stream eventへ出した後も動き続けるfake childを本物runnerで実行し、最初のsignal
// 観測でchildが終了し、追加retryなしにRATE_LIMITED checkpoint・session・reset時刻
// auto-resumeが一度だけ保存されることをproduction pathで固定する。
func TestFiveHourLimitEarlyStopSavesRateLimitedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	const holdSeconds = 60
	const elapsedBound = 30 * time.Second
	const signal = "API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-08-23 12:00:00]"

	cases := []struct {
		name string
		emit string
	}{
		{"stderr", "echo '" + signal + "' >&2\n"},
		{"json stream event", "printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"error\",\"message\":\"" + signal + "\"}'\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runFiveHourLimitEarlyStopCase(t, holdSeconds, elapsedBound, c.emit)
		})
	}
}

func runFiveHourLimitEarlyStopCase(t *testing.T, holdSeconds int, elapsedBound time.Duration, emit string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "after-limit")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	script := "#!/bin/sh\n" +
		emit +
		"sleep " + strconv.Itoa(holdSeconds) + "\n" +
		"touch \"" + marker + "\"\n" +
		"exit 1\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	st := newStateStoreT(t)
	repoRoot := t.TempDir()
	adapter := &processModelRunner{inner: runner.NewClaudeRunner(config.AppConfig{
		RepoRoot:        repoRoot,
		RepoShort:       "testrepo1234",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
	}, st)}
	w := NewWorkflow(config.AppConfig{
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		TelemetryContent:      true,
		RepoRoot:              repoRoot,
		RepoShort:             "testrepo1234",
	}, st, adapter, io.Discard)
	w.temp = t.TempDir()

	started := time.Now()
	_, err := w.runModel(workerCheckpoint())
	elapsed := time.Since(started)

	var limitErr runner.ZaiRateLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("ZaiRateLimitErrorを期待: %v", err)
	}
	if limitErr.Phase != "worker-new" {
		t.Fatalf("phase = %q", limitErr.Phase)
	}
	if limitErr.Limit.ResetAtRFC3339 != "2026-08-23T12:00:00+08:00" {
		t.Fatalf("reset時刻 = %#v", limitErr.Limit)
	}
	if scheduled, at := limitErr.AutoResumeSchedule(); !scheduled || at != "2026-08-23T12:02:00+08:00" {
		t.Fatalf("auto-resume予定 = %v/%q", scheduled, at)
	}
	if adapter.calls != 1 {
		t.Fatalf("5h limit検出後の追加model呼出 = %d (retry 0回である必要)", adapter.calls)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("signal観測後にchildが後続処理へ進みました: %v", statErr)
	}
	if elapsed >= elapsedBound {
		t.Fatalf("早期停止後%.1f秒で復帰していません(保持%d秒を待った)", elapsed.Seconds(), holdSeconds)
	}

	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.RateLimited || cp.ProviderUnavailable {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if cp.ResetAtCST != "2026-08-23 12:00:00" || cp.ResetAtRFC3339 != "2026-08-23T12:00:00+08:00" {
		t.Fatalf("checkpoint reset時刻 = %#v", cp)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if !st.Exists("worker.ready") || !st.Exists("worker.id") {
		t.Fatal("5h limit停止でsessionが破棄されました")
	}
	stats, statsErr := st.CurrentTaskStats()
	if statsErr != nil || stats.RateLimits != 1 {
		t.Fatalf("RATE_LIMITED保存回数 = %d err=%v (exactly once)", stats.RateLimits, statsErr)
	}
	if key := limitErr.AutoResumeKey(); !strings.Contains(key, "testrepo1234") {
		t.Fatalf("auto-resume key = %q", key)
	}
}
