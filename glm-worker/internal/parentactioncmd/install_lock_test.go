package parentactioncmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type installExecutionResult struct {
	stdout string
	stderr string
	err    error
}

func startBlockingInstall(cfg config.AppConfig) <-chan installExecutionResult {
	result := make(chan installExecutionResult, 1)
	go func() {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		err := execute(cfg, []string{"install"}, &stdout, &stderr)
		result <- installExecutionResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
	}()
	return result
}

func waitForInstallPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("path was not created: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func releaseBlockingInstall(t *testing.T, cfg config.AppConfig, result <-chan installExecutionResult) installExecutionResult {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "install-release"), []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case observed := <-result:
		return observed
	case <-time.After(5 * time.Second):
		t.Fatal("install child did not exit after release")
		return installExecutionResult{}
	}
}

func assertRepositoryLockAvailable(t *testing.T, st *state.StateStore) {
	t.Helper()
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatalf("repository lock remained held: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteInstallBlocksConcurrentCompleteUntilChildExit(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\ntouch install-started\nwhile [ ! -e install-release ]; do sleep 0.01; done\nexit 0\n", 0o755)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	result := startBlockingInstall(cfg)
	waitForInstallPath(t, filepath.Join(cfg.RepoRoot, "install-started"))

	var completeOutput bytes.Buffer
	if err := runComplete(cfg, &completeOutput); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("concurrent complete error = %v output = %q", err, completeOutput.String())
	}
	if status := st.TaskStatus(); status != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("concurrent complete changed task status: %s", status)
	}

	observed := releaseBlockingInstall(t, cfg, result)
	if observed.err != nil {
		t.Fatalf("install error = %v stderr = %q", observed.err, observed.stderr)
	}
	if !strings.Contains(observed.stdout, "installed") {
		t.Fatalf("install stdout = %q", observed.stdout)
	}
	assertRepositoryLockAvailable(t, st)
}

func TestExecuteInstallRejectsConcurrentInstallWhileChildRuns(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nprintf 'run\\n' >> install-runs\ntouch install-started\nwhile [ ! -e install-release ]; do sleep 0.01; done\nexit 0\n", 0o755)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	first := startBlockingInstall(cfg)
	waitForInstallPath(t, filepath.Join(cfg.RepoRoot, "install-started"))

	var secondStdout bytes.Buffer
	var secondStderr bytes.Buffer
	if err := execute(cfg, []string{"install"}, &secondStdout, &secondStderr); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("concurrent install error = %v stdout = %q stderr = %q", err, secondStdout.String(), secondStderr.String())
	}

	observed := releaseBlockingInstall(t, cfg, first)
	if observed.err != nil {
		t.Fatalf("first install error = %v stderr = %q", observed.err, observed.stderr)
	}
	runs, err := os.ReadFile(filepath.Join(cfg.RepoRoot, "install-runs"))
	if err != nil {
		t.Fatal(err)
	}
	if string(runs) != "run\n" {
		t.Fatalf("install child executions = %q", runs)
	}
	assertRepositoryLockAvailable(t, st)
}

func TestExecuteInstallReleasesRepositoryLockOnRejectedAndFailedPaths(t *testing.T) {
	t.Run("admission rejected", func(t *testing.T) {
		cfg, st := newInstallActionRepo(t)
		writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o755)
		if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
			t.Fatal(err)
		}
		if _, _, err := runInstallAction(t, cfg); err == nil {
			t.Fatal("install was admitted for complete task")
		}
		assertRepositoryLockAvailable(t, st)
	})

	t.Run("guard rejected", func(t *testing.T) {
		cfg, st := newInstallActionRepo(t)
		if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
			t.Fatal(err)
		}
		output, _, err := runInstallAction(t, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if output.Status != installStatusGuardRejected {
			t.Fatalf("guard output = %#v", output)
		}
		assertRepositoryLockAvailable(t, st)
	})

	t.Run("child failed", func(t *testing.T) {
		cfg, st := newInstallActionRepo(t)
		writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 3\n", 0o755)
		if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
			t.Fatal(err)
		}
		output, _, err := runInstallAction(t, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if output.Status != installStatusFailed {
			t.Fatalf("child failure output = %#v", output)
		}
		assertRepositoryLockAvailable(t, st)
	})
}
