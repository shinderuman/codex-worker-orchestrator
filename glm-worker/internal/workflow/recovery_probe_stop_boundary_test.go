//go:build unix

package workflow

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecoveryProbeStopUsesRunnerProcessGroupAndPersistsInterruptedCheckpoint(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	commandPath := filepath.Join(t.TempDir(), "hanging-probe")
	pidPath := filepath.Join(t.TempDir(), "probe.pid")
	readyPath := filepath.Join(t.TempDir(), "probe-child.ready")
	script := `#!/bin/sh
trap '' TERM
echo $$ > "$GLM_PROBE_PID"
(
  trap '' TERM
  : > "$GLM_PROBE_CHILD_READY"
  while :; do sleep 0.2; done
) &
while :; do sleep 0.2; done
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_PROBE_PID", pidPath)
	t.Setenv("GLM_PROBE_CHILD_READY", readyPath)

	cfg := config.AppConfig{
		RepoRoot:        repo,
		RepoShort:       "abcdef123456",
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
		EnvAllowlist:    []string{"GLM_PROBE_PID", "GLM_PROBE_CHILD_READY"},
		WorkerModel:     "opus",
		RoutineEffort:   "high",
	}
	base := runner.NewClaudeRunner(cfg, st)
	stop := runner.NewStopController()
	base.AttachStopController(stop)
	modelRunner := runner.NewInstructionSurfaceGuardRunner(base)
	w := NewWorkflow(cfg, st, modelRunner, io.Discard)
	w.AttachStopController(stop)
	w.temp = t.TempDir()

	checkpoint := state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		ProviderUnavailableClassification: "http-503",
	}
	checkpoint.SetStopKind(state.ResumeStopProviderUnavailable)
	if err := st.EnterStop(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.BeginResume(checkpoint); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() { result <- w.gateResumeProvider(checkpoint) }()
	pgid := waitRecoveryProbePID(t, pidPath)
	waitRecoveryProbeFile(t, readyPath)
	stop.Request()

	select {
	case err := <-result:
		var interrupted *runner.InterruptedCallError
		if !errors.As(err, &interrupted) {
			t.Fatalf("InterruptedCallErrorを期待: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		t.Fatal("recovery probe停止が完了しません")
	}

	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if saved.StopKind != state.ResumeStopInterrupted {
		t.Fatalf("stop kind = %s, want %s", saved.StopKind, state.ResumeStopInterrupted)
	}
	if saved.Phase != checkpoint.Phase || saved.Model != checkpoint.Model || saved.Role != checkpoint.Role {
		t.Fatalf("checkpoint identityが変化しました: %#v", saved)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("task status = %s, want %s", st.TaskStatus(), state.TaskStatusInterrupted)
	}

	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(-pgid, syscall.Signal(0)) == nil {
		if !time.Now().Before(deadline) {
			t.Fatalf("probe process group %dが停止後も残っています", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitRecoveryProbePID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("probeがPID file %sを書きません", path)
	return 0
}

func waitRecoveryProbeFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("probe childがready file %sを書きません", path)
}
