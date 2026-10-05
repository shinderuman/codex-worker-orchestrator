package state

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestControllerRuntimeBindingRoundTripAndWorkflowTaskPath(t *testing.T) {
	st := newControllerRuntimeBindingTestStore(t)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/plan.md"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.CurrentWorkflowTaskPath(); err != nil || got != "IMPLEMENTATION_TASKS/plan.md" {
		t.Fatalf("plan task fallback = %q, %v", got, err)
	}
	binding := ControllerRuntimeBinding{AttemptID: "attempt-1", TaskPath: "IMPLEMENTATION_TASKS/controller.md", TaskContractDigest: "digest-1"}
	if err := st.SaveControllerRuntimeBinding(binding); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadControllerRuntimeBinding()
	if err != nil || got != binding {
		t.Fatalf("binding round trip = %#v, %v", got, err)
	}
	if path, err := st.CurrentWorkflowTaskPath(); err != nil || path != binding.TaskPath {
		t.Fatalf("controller task = %q, %v", path, err)
	}
}

func TestControllerRuntimeBindingRejectsCorruptStateWithoutActiveTaskFallback(t *testing.T) {
	st := newControllerRuntimeBindingTestStore(t)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/plan.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(controllerRuntimeBindingStateFile), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CurrentWorkflowTaskPath(); err == nil {
		t.Fatal("corrupt controller runtime binding fell back to active-task")
	}
}

func newControllerRuntimeBindingTestStore(t *testing.T) *StateStore {
	t.Helper()
	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "repo"}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
