package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentWaitBlocksOnPrimaryOwnerWithoutOutput(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	writeParentWaitWorkerStub(t)

	owner, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- executeParentWait(cfg, []string{"wait"}, &stdout, &stderr) }()

	select {
	case err := <-done:
		t.Fatalf("wait returned while primary owner was active: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if stdout.Len() != 0 {
		t.Fatalf("wait emitted liveness output while unchanged: %q", stdout.String())
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after primary owner released")
	}
	result := decodeParentWaitOutput(t, stdout.Bytes())
	if result.Status != parentWaitStatusReleased || result.TaskStatus != state.TaskStatusWaitingSolReview || result.OwnerLost {
		t.Fatalf("wait output = %#v", result)
	}
}

func TestParentWaitBlocksOnSurvivingWorkerAfterParentOwnerLoss(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	writeParentWaitWorkerStub(t)

	worker, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- executeParentWait(cfg, []string{"wait"}, &stdout, &stderr) }()

	select {
	case err := <-done:
		t.Fatalf("wait returned while surviving worker held repo lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if stdout.Len() != 0 {
		t.Fatalf("wait emitted output before worker terminal: %q", stdout.String())
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after worker released")
	}
}

func TestParentWaitMarksLostOwnerInsteadOfRestartingActiveTask(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	writeParentWaitWorkerStub(t)

	var stdout, stderr bytes.Buffer
	if err := executeParentWait(cfg, []string{"wait"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	result := decodeParentWaitOutput(t, stdout.Bytes())
	if !result.OwnerLost || result.TaskStatus != state.TaskStatusActive {
		t.Fatalf("lost owner output = %#v", result)
	}
}

func TestParentWaitLeaseRejectsDuplicateModelRunningOwner(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withParentWaitLease(cfg, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	if err := withParentWaitLease(cfg, func() error { return nil }); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("duplicate parent owner error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestParentWaitRejectsExtraArguments(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	if err := executeParentWait(cfg, []string{"wait", "extra"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("extra wait argument was accepted")
	}
}

func writeParentWaitWorkerStub(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "${1:-}" = "--handoff" ] && [ "${2:-}" = "recovery" ]; then
  printf '%s\n' '{"consistent":true,"required_action":"parent-review","allowed_actions":["accept"]}'
  exit 0
fi
exit 2
`
	if err := os.WriteFile(filepath.Join(bin, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func decodeParentWaitOutput(t *testing.T, data []byte) parentWaitOutput {
	t.Helper()
	var result parentWaitOutput
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("wait output is not JSON: %v: %q", err, data)
	}
	if len(result.Handoff) == 0 {
		t.Fatalf("wait output has no handoff: %q", data)
	}
	return result
}
