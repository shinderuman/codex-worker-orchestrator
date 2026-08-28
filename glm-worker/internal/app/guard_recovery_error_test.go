package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func TestGuardRecoverableProcessError(t *testing.T) {
	body := buildProcessError(&workflow.GuardRecoverableError{
		Phase:       "worker-new",
		TaskID:      "task-1",
		RepoRoot:    "/repo",
		ResultSaved: true,
		Failure:     "git authority guard failed: blocked-command",
	})
	if body.Kind != errorKindGuardRecoverable {
		t.Fatalf("kind = %q", body.Kind)
	}
	if body.Detail["resume_available"] != true {
		t.Fatalf("resume_available = %#v", body.Detail["resume_available"])
	}
	if body.Detail["completed_result_saved"] != true {
		t.Fatalf("completed_result_saved = %#v", body.Detail["completed_result_saved"])
	}
	assertGuardRecoveryDetailString(t, body.Detail, "phase", "worker-new")
	assertGuardRecoveryDetailString(t, body.Detail, "task_id", "task-1")
	assertGuardRecoveryDetailString(t, body.Detail, "repo_root", "/repo")
}

func assertGuardRecoveryDetailString(t *testing.T, detail map[string]any, key, want string) {
	t.Helper()
	got, ok := detail[key].(*string)
	if !ok || got == nil || *got != want {
		t.Fatalf("%s = %#v want %q", key, detail[key], want)
	}
}
