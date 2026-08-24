package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// statusは対象repo lockと独立に、別repoのstate/lockの影響を受けない。
func TestExecuteStatusShowsRepositoryLockFreeByDefault(t *testing.T) {
	cfg := newAppConfig(t)
	output := executeStatusOutput(t, cfg)
	if output.RepositoryLock != "free" {
		t.Fatalf("空状態のrepository_lock = %q", output.RepositoryLock)
	}
	if output.TaskLiveness != "" {
		t.Fatalf("task_statusがactiveでない状態でtask_liveness = %q", output.TaskLiveness)
	}
}

func TestExecuteStatusActiveWithoutLockIsStaleCandidate(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	if output.TaskStatus != string(state.TaskStatusActive) {
		t.Fatalf("task_status = %q", output.TaskStatus)
	}
	if output.RepositoryLock != "free" {
		t.Fatalf("repository_lock = %q", output.RepositoryLock)
	}
	if output.TaskLiveness != "stale" {
		t.Fatalf("task_liveness = %q", output.TaskLiveness)
	}
	if output.ResumeAvailable {
		t.Fatal("停止理由がないのにresume_availableです")
	}
}

func TestExecuteStatusActiveWithLockHeldIsRunning(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	lock, err := AcquireRepoLock(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	output := executeStatusOutput(t, cfg)
	if output.RepositoryLock != "held" {
		t.Fatalf("repository_lock = %q", output.RepositoryLock)
	}
	if output.TaskLiveness != "running" {
		t.Fatalf("task_liveness = %q", output.TaskLiveness)
	}
}

// status観測後に同じrepoで次commandを実行すると、lock取得成否だけで安全に収束する。
// 別repoのlockは同一commandの可否に影響しない。
func TestExecuteStatusRaceConvergesOnNextCommandLock(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = executeStatusOutput(t, cfg)

	otherCfg := cfg
	otherCfg.RepoHash = cfg.RepoHash + "-other"
	otherCfg.RepoShort = cfg.RepoShort + "-oth"
	otherSt, err := state.NewStateStore(otherCfg)
	if err != nil {
		t.Fatal(err)
	}
	otherLock, err := AcquireRepoLock(otherSt.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer otherLock.Close()

	var resetOut bytes.Buffer
	if err := Execute(Command{Mode: ModeReset}, cfg, nil, &resetOut, io.Discard); err != nil {
		t.Fatalf("別repo lock保持中に対象repoの次commandが失敗しました: %v", err)
	}
	var reset map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(resetOut.String())), &reset); err != nil {
		t.Fatalf("reset出力がmachine JSONではありません: %v: %q", err, resetOut.String())
	}
	if reset["status"] != "reset" {
		t.Fatalf("reset出力 = %v", reset)
	}
	_ = st
}

// TASK_STATUSがactive以外ではtask_livenessを出さない。
func TestExecuteStatusHidesLivenessForNonActiveTask(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	if output.TaskLiveness != "" {
		t.Fatalf("非active taskでtask_liveness = %q", output.TaskLiveness)
	}
	if output.RepositoryLock != "free" {
		t.Fatalf("repository_lock = %q", output.RepositoryLock)
	}
}

// checkpointを持つrate-limited taskのresume表示はliveness追加後も不変。
func TestExecuteStatusRateLimitedResumeFieldsUnchanged(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:       state.ResumeStageWorker,
		Phase:       "worker-new",
		Role:        state.WorkerRole,
		Model:       "opus",
		Effort:      "high",
		Prompt:      "p",
		Request:     "req",
		RateLimited: true,
		ResetAtCST:  "2026-08-15 10:00:00",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	if output.TaskStatus != string(state.TaskStatusRateLimited) {
		t.Fatalf("task_status = %q", output.TaskStatus)
	}
	if !output.RateLimited.Limited {
		t.Fatalf("rate_limited = %#v", output.RateLimited)
	}
	if !output.ResumeAvailable {
		t.Fatal("rate-limited taskはresume_availableが必要です")
	}
	if output.TaskLiveness != "" {
		t.Fatalf("rate-limited taskでtask_liveness = %q", output.TaskLiveness)
	}
}
