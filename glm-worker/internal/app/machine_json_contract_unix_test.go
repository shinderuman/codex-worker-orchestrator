//go:build unix

package app

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStatusRawJSONLockProbeUnknownIsNull(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lock", st.LockPath()); err != nil {
		t.Fatal(err)
	}
	if probe := ProbeRepoLock(st.LockPath()); probe.State != LockUnknown {
		t.Skipf("この環境ではprobe不能状態を作れません: %s", probe.State)
	}

	decoded := statusRawJSON(t, cfg)
	assertNullJSONValue(t, "repository_lock", requireJSONKey(t, decoded, "repository_lock"))
	assertNullJSONValue(t, "lock_pid", requireJSONKey(t, decoded, "lock_pid"))
	assertNullJSONValue(t, "task_status", requireJSONKey(t, decoded, "task_status"))
	assertNullJSONValue(t, "task_liveness", requireJSONKey(t, decoded, "task_liveness"))
	assertNoPresentationSentinel(t, decoded, "repository_lock", "lock_pid", "task_status", "task_liveness")
}
