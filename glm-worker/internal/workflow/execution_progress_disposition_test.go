package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestProjectExecutionProgressDistinguishesExplicitSingleFromUntracked(t *testing.T) {
	_, st, _, _ := newExecutionMilestoneWorkflow(t, nil)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/current.md"
	if err := st.Write(activeTaskStateKey, taskPath); err != nil {
		t.Fatal(err)
	}
	if err := executionunit.RecordDisposition(st, taskPath, executionunit.ExecutionUnitSingle, testFixedTime); err != nil {
		t.Fatal(err)
	}

	got := ProjectExecutionProgress(st, "worker-new", string(state.WorkerRole))
	if got.Status != "indeterminate" || got.Precision != "unavailable" || got.Band != "" {
		t.Fatalf("progress = %#v", got)
	}
	if got.Basis != "explicit-single-execution-unit" || got.Reason != "single-execution-unit" {
		t.Fatalf("explicit single evidence = %#v", got)
	}
}
