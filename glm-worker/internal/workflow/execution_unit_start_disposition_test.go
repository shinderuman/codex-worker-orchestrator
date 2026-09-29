package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
)

func TestRecordInitialExecutionUnitDispositionPersistsExplicitSingle(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/current.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(executionunit.DispositionEnv, executionunit.ExecutionUnitSingle)
	if err := w.recordInitialExecutionUnitDisposition(taskPath); err != nil {
		t.Fatal(err)
	}

	got, err := executionunit.CurrentDisposition(st)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ExecutionUnit != executionunit.ExecutionUnitSingle || got.ActiveTaskPath != taskPath {
		t.Fatalf("disposition = %#v", got)
	}
}

func TestRecordInitialExecutionUnitDispositionRejectsUnsupportedMode(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/current.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(executionunit.DispositionEnv, "unknown")
	if err := w.recordInitialExecutionUnitDisposition(taskPath); err == nil {
		t.Fatal("unsupported start disposition was accepted")
	}
}
