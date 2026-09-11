package parentactioncmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentWaitHoldsRepositoryLockThroughRecoveryHandoff(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	entered := filepath.Join(t.TempDir(), "handoff-entered")
	release := filepath.Join(t.TempDir(), "handoff-release")
	writeBlockingParentWaitHandoffStub(t, entered, release)

	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- executeParentWait(cfg, []string{"wait"}, &stdout, &stderr) }()
	waitForFile(t, entered)

	lock, err := repolock.Acquire(st.LockPath())
	if !errors.Is(err, repolock.ErrRepoLockHeld) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("repository lock was released before recovery handoff snapshot completed: %v", err)
	}
	if err := os.WriteFile(release, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parent wait did not return after recovery handoff completed")
	}
}

func TestValidateParentWaitRecoveryHandoffRejectsIncompleteObject(t *testing.T) {
	if err := validateParentWaitRecoveryHandoff([]byte(`{}`)); err == nil {
		t.Fatal("empty recovery handoff must be rejected")
	}
	valid := []byte(`{"projection":"recovery","consistent":true,"task_id":"task-1","task_status":"waiting-sol-review","required_action":"parent-review","allowed_actions":["accept"]}`)
	if err := validateParentWaitRecoveryHandoff(valid); err != nil {
		t.Fatalf("valid recovery handoff rejected: %v", err)
	}
}

func writeBlockingParentWaitHandoffStub(t *testing.T, entered, release string) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "--handoff" ] && [ "${2:-}" = "recovery" ]; then
  touch "$GLM_HANDOFF_ENTERED"
  while [ ! -f "$GLM_HANDOFF_RELEASE" ]; do
    sleep 0.01
  done
  printf '%s\n' '{"projection":"recovery","consistent":true,"task_id":"task-1","task_status":"waiting-sol-review","required_action":"parent-review","allowed_actions":["accept"]}'
  exit 0
fi
exit 2
`
	if err := os.WriteFile(filepath.Join(bin, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GLM_HANDOFF_ENTERED", entered)
	t.Setenv("GLM_HANDOFF_RELEASE", release)
}
