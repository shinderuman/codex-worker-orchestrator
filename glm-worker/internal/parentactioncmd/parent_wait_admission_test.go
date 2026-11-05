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

// 親ownerだけの終了はmodel再入境界ではない。Workerのin-flightを維持し、
// 正規の中断とrepository lock解放を受けて初めて復旧handoffを返す。
func TestParentWaitAdmissionRetainsWorkerAndDeliversInterruption(t *testing.T) {
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	seedParentWaitOwnerEpoch(t, st)
	writeInterruptedParentWaitWorkerStub(t)

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
	for _, tc := range []struct {
		name   string
		script string
	}{
		{
			name:   "invalid-json",
			script: "#!/bin/sh\\nprintf '%s\\\\n' '{not-json'\\n",
		},
		{
			name:   "worker-failed",
			script: "#!/bin/sh\\nexit 2\\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st := newParentActionTestState(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
				t.Fatal(err)
			}
			seedParentWaitOwnerEpoch(t, st)

			bin := t.TempDir()
			if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(tc.script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			var stdout, stderr bytes.Buffer
			if err := executeParentWait(cfg, []string{"wait"}, &stdout, &stderr); err == nil {
				t.Fatal("recovery error was treated as successful terminal")
			}
			if stdout.Len() != 0 {
				t.Fatalf("recovery error produced a false release: %s", stdout.String())
			}
			if status := st.TaskStatus(); status != state.TaskStatusWaitingSolReview {
				t.Fatalf("recovery error mutated task status: %s", status)
			}
		})
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

func writeInterruptedParentWaitWorkerStub(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "${1:-}" = "--handoff" ] && [ "${2:-}" = "recovery" ]; then
  printf '%s\\n' '{"projection":"recovery","consistent":true,"task_id":"task-1","task_status":"interrupted","required_action":"resume","allowed_actions":["resume"]}'
  exit 0
fi
exit 2
`
	if err := execfile.Write(filepath.Join(bin, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
