//go:build unix

package app

import (
	"context"
	"strings"
	"testing"
)

func TestOwnerLostActiveTaskRejectsNewTask(t *testing.T) {
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
	if probe := ProbeRepoLock(env.workflowLockPath(t, env.repoA)); probe.State != LockFree {
		t.Fatalf("owner loss後にrepository lockが解放されていません: %s", probe.State)
	}
	status := env.status(t, env.repoA)
	if got := statusJSONField(t, status, "task_status"); got != "active" {
		t.Fatalf("owner loss後のtask status = %v", got)
	}

	denied := env.run(t, env.repoA, "replacement task marker")
	if denied.code != 1 || !strings.Contains(denied.stderr, "controller-semantic") {
		t.Fatalf("active task上のnew-task startが拒否されません: code=%d stderr=%s", denied.code, denied.stderr)
	}
	if got := readStateFile(t, stateDir, "task.id"); got != taskID {
		t.Fatalf("拒否されたstartがtask.idを変更しました: want=%s got=%s", taskID, got)
	}
	if got := readStateFile(t, stateDir, "last-request"); got != request {
		t.Fatalf("拒否されたstartがtask requestを変更しました: want=%q got=%q", request, got)
	}
}
