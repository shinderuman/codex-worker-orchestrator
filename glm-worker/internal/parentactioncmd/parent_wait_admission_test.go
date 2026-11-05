package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// The primary owner ending is not a model decision boundary when its worker is
// still in flight. An interruption must become visible only after the worker
// releases the repository lock, without an intermediate liveness response.
func TestParentWaitAdmissionRetainsWorkerAndDeliversInterruption(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	seedParentWaitOwnerEpoch(t, st)
	writeParentWaitAdmissionWorkerStub(t, "interrupted")

	owner, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close() }()
	worker, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Close() }()

	var stdout parentWaitOutputBuffer
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- executeParentWait(cfg, []string{"wait"}, &stdout, &stderr) }()
	waitForParentRecoveryWaiter(t, st, done)
	assertParentWaitStillBlocked(t, done, &stdout)

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	assertParentWaitStillBlocked(t, done, &stdout)

	// Simulate the canonical worker stop transition before it releases the lock.
	if err := st.SetTaskStatus(state.TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("recovery after interruption: %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted worker did not release recovery waiter")
	}
	result := decodeParentWaitOutput(t, stdout.Bytes())
	if result.Status != parentWaitStatusReleased || result.OwnerLost || result.TaskStatus != state.TaskStatusInterrupted {
		t.Fatalf("interruption was not preserved: %#v", result)
	}
	var handoff parentWaitRecoveryHandoff
	if err := json.Unmarshal(result.Handoff, &handoff); err != nil {
		t.Fatal(err)
	}
	if handoff.TaskStatus == nil || *handoff.TaskStatus != string(state.TaskStatusInterrupted) {
		t.Fatalf("interrupted handoff was not delivered: %s", result.Handoff)
	}
}

func TestParentWaitAdmissionSurfacesRecoveryErrorWithoutFalseRelease(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	seedParentWaitOwnerEpoch(t, st)

	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '{not-json'\n"
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	if err := executeParentWait(cfg, []string{"wait"}, &stdout, &stderr); err == nil {
		t.Fatal("invalid recovery handoff was treated as successful terminal")
	}
	if stdout.Len() != 0 {
		t.Fatalf("invalid handoff produced a false release: %s", stdout.String())
	}
	if status := st.TaskStatus(); status != state.TaskStatusWaitingSolReview {
		t.Fatalf("recovery failure mutated task status: %s", status)
	}
}

func assertParentWaitStillBlocked(t *testing.T, done <-chan error, stdout *parentWaitOutputBuffer) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("unchanged wait returned to parent model: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if stdout.Len() != 0 {
		t.Fatalf("unchanged wait emitted liveness output: %s", stdout.Bytes())
	}
}

func writeParentWaitAdmissionWorkerStub(t *testing.T, taskStatus string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"\u0024{1:-}\" = \"--handoff\" ] && [ \"\u0024{2:-}\" = \"recovery\" ]; then\n" +
		"  printf '%s\\n' '{\"projection\":\"recovery\",\"consistent\":true,\"task_id\":\"task-1\",\"task_status\":\"" + taskStatus + "\",\"required_action\":\"resume\",\"allowed_actions\":[\"resume\"]}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 2\n"
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
