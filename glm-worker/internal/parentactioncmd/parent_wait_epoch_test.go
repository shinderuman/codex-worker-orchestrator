package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentWaitLeaseRotatesOwnerEpoch(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	var first string
	if err := withParentWaitLease(cfg, func() error {
		var err error
		first, err = readParentWaitOwnerEpoch(st)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var second string
	if err := withParentWaitLease(cfg, func() error {
		var err error
		second, err = readParentWaitOwnerEpoch(st)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("owner epoch did not rotate: %s", first)
	}
}

func TestParentWaitReturnsSupersededWhenOwnerEpochChanges(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	first := seedParentWaitOwnerEpoch(t, st)

	owner, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- executeParentWait(cfg, []string{"wait"}, &stdout, &stderr) }()
	waitForParentRecoveryWaiter(t, st)

	time.Sleep(50 * time.Millisecond)
	second := seedParentWaitOwnerEpoch(t, st)
	if first == second {
		t.Fatalf("owner epoch did not change: %s", first)
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
		t.Fatal("superseded recovery waiter did not return")
	}

	var result parentWaitOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("superseded output is not JSON: %v: %q", err, stdout.Bytes())
	}
	if result.Status != parentWaitStatusSuperseded {
		t.Fatalf("superseded recovery status = %q", result.Status)
	}
	if len(result.Handoff) != 0 {
		t.Fatalf("superseded recovery emitted a handoff: %s", result.Handoff)
	}
}

func TestParentWaitRequiresOwnerEpoch(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := executeParentWait(cfg, []string{"wait"}, &stdout, &stderr); err == nil {
		t.Fatal("recovery wait without owner epoch was accepted")
	}
	if stdout.Len() != 0 {
		t.Fatalf("missing owner epoch emitted output: %q", stdout.String())
	}
}

func seedParentWaitOwnerEpoch(t *testing.T, st *state.StateStore) string {
	t.Helper()
	epoch, err := rotateParentWaitOwnerEpoch(st)
	if err != nil {
		t.Fatal(err)
	}
	return epoch
}
