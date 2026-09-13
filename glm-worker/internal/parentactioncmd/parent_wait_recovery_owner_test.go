package parentactioncmd

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentWaitRejectsDuplicateRecoveryWaiterBehindPrimaryOwner(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	seedParentWaitOwnerEpoch(t, st)
	writeParentWaitWorkerStub(t)

	owner, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		t.Fatal(err)
	}
	var firstStdout, firstStderr bytes.Buffer
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- executeParentWait(cfg, []string{"wait"}, &firstStdout, &firstStderr)
	}()
	waitForParentRecoveryWaiter(t, st, firstDone)

	var duplicateStdout, duplicateStderr bytes.Buffer
	if err := executeParentWait(cfg, []string{"wait"}, &duplicateStdout, &duplicateStderr); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("duplicate recovery waiter error = %v", err)
	}
	if duplicateStdout.Len() != 0 {
		t.Fatalf("duplicate recovery waiter emitted output: %q", duplicateStdout.String())
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("eligible recovery waiter did not return after primary owner released")
	}
	result := decodeParentWaitOutput(t, firstStdout.Bytes())
	if result.Status != parentWaitStatusReleased {
		t.Fatalf("eligible recovery waiter output = %#v", result)
	}
}

func TestParentWaitRejectsDuplicateRecoveryWaiterBehindSurvivingWorker(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	seedParentWaitOwnerEpoch(t, st)
	writeParentWaitWorkerStub(t)

	worker, err := repolock.Acquire(st.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	var firstStdout, firstStderr bytes.Buffer
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- executeParentWait(cfg, []string{"wait"}, &firstStdout, &firstStderr)
	}()
	waitForParentRecoveryWaiter(t, st, firstDone)

	var duplicateStdout, duplicateStderr bytes.Buffer
	if err := executeParentWait(cfg, []string{"wait"}, &duplicateStdout, &duplicateStderr); !errors.Is(err, repolock.ErrRepoLockHeld) {
		t.Fatalf("duplicate recovery waiter error = %v", err)
	}
	if duplicateStdout.Len() != 0 {
		t.Fatalf("duplicate recovery waiter emitted output: %q", duplicateStdout.String())
	}

	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("eligible recovery waiter did not return after surviving worker released")
	}
	result := decodeParentWaitOutput(t, firstStdout.Bytes())
	if result.Status != parentWaitStatusReleased {
		t.Fatalf("eligible recovery waiter output = %#v", result)
	}
}

func waitForParentRecoveryWaiter(t *testing.T, st *state.StateStore, done <-chan error) {
	t.Helper()
	path := st.Path(parentWaitRecoveryLockFile)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("parent recovery waiter returned before lease became observable: %v", err)
		default:
		}
		data, err := os.ReadFile(path)
		switch {
		case err == nil && len(bytes.TrimSpace(data)) > 0:
			return
		case err == nil:
		case errors.Is(err, os.ErrNotExist):
		default:
			t.Fatalf("read parent recovery waiter lease marker: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for parent recovery waiter lease")
}
