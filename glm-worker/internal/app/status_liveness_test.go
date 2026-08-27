package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteStatusShowsRepositoryLockFreeByDefault(t *testing.T) {
	cfg := newAppConfig(t)
	output := executeStatusOutput(t, cfg)
	statusString(t, "repository_lock", output.RepositoryLock, "free")
	statusNullString(t, "task_liveness", output.TaskLiveness)
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
	statusString(t, "task_status", output.TaskStatus, string(state.TaskStatusActive))
	statusString(t, "repository_lock", output.RepositoryLock, "free")
	statusString(t, "task_liveness", output.TaskLiveness, "stale")
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
	defer func() { _ = lock.Close() }()

	output := executeStatusOutput(t, cfg)
	statusString(t, "repository_lock", output.RepositoryLock, "held")
	statusString(t, "task_liveness", output.TaskLiveness, "running")
}

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
	defer func() { _ = otherLock.Close() }()

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
	statusNullString(t, "task_liveness", output.TaskLiveness)
	statusString(t, "repository_lock", output.RepositoryLock, "free")
}

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
	statusString(t, "task_status", output.TaskStatus, string(state.TaskStatusRateLimited))
	if !output.RateLimited.Limited {
		t.Fatalf("rate_limited = %#v", output.RateLimited)
	}
	if !output.ResumeAvailable {
		t.Fatal("rate-limited taskはresume_availableが必要です")
	}
	statusNullString(t, "task_liveness", output.TaskLiveness)
}
