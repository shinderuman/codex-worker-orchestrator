//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerLostActiveTaskRequiresExplicitResetBeforeNewTask(t *testing.T) {
	env := newMultiRepoEnv(t)
	env.setStubMode(t, env.stubA, "hold")

	holdCtx, cancelHold := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	defer cancelHold()
	holder := env.start(t, holdCtx, env.repoA, "owner-lost task marker")
	stateDir := env.waitStateDir(t, env.repoA, holder)
	env.waitHeldWithWorkerSession(t, stateDir)
	taskID := readStateFile(t, stateDir, "task.id")
	request := readStateFile(t, stateDir, "last-request")

	env.releaseHold(t)
	holder.waitFailure(t)
	if probe := ProbeRepoLock(filepath.Join(stateDir, "lock")); probe.State != LockFree {
		t.Fatalf("owner loss後にrepository lockが解放されていません: %s", probe.State)
	}
	status := env.status(t, env.repoA)
	if got := statusJSONField(t, status, "task_status"); got != "active" {
		t.Fatalf("owner loss後のtask status = %v", got)
	}

	denied := env.run(t, env.repoA, "replacement task marker")
	if denied.code != 1 || !strings.Contains(denied.stderr, "glm-worker --reset") {
		t.Fatalf("active task上のnew-task startが拒否されません: code=%d stderr=%s", denied.code, denied.stderr)
	}
	if got := readStateFile(t, stateDir, "task.id"); got != taskID {
		t.Fatalf("拒否されたstartがtask.idを変更しました: want=%s got=%s", taskID, got)
	}
	if got := readStateFile(t, stateDir, "last-request"); got != request {
		t.Fatalf("拒否されたstartがtask requestを変更しました: want=%q got=%q", request, got)
	}

	reset := env.run(t, env.repoA, "--reset")
	if reset.code != 0 || !strings.Contains(reset.stdout, `"status":"reset"`) {
		t.Fatalf("owner-lost active taskのresetが失敗しました: code=%d stdout=%s stderr=%s", reset.code, reset.stdout, reset.stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "task.id")); !os.IsNotExist(err) {
		t.Fatalf("reset後もtask.idが残っています: %v", err)
	}

	env.setStubMode(t, env.stubA, "success")
	started := env.run(t, env.repoA, "replacement task marker")
	if started.code != 0 || !strings.Contains(started.stdout, `"status":"PASS"`) {
		t.Fatalf("明示reset後のnew-task startが完結しません: code=%d stdout=%s stderr=%s", started.code, started.stdout, started.stderr)
	}
	if got := readStateFile(t, stateDir, "task.id"); got == taskID {
		t.Fatalf("reset後のnew taskが旧task.idを再利用しました: %s", got)
	}
}
