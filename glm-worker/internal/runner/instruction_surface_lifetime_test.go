package runner

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInstructionSurfaceBaselineSurvivesFreshTaskAndRebindsByTaskID(t *testing.T) {
	if instructionSurfaceBaselineStateKey != state.InstructionSurfaceBaselineStateFile {
		t.Fatalf("instruction surface state key is not registered in task lifetime policy: %q", instructionSurfaceBaselineStateKey)
	}

	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "task-one")
	r := newInstructionGuardRunner(t, root, "task-one")
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	oldBaseline, err := r.state.Read(instructionSurfaceBaselineStateKey)
	if err != nil {
		t.Fatal(err)
	}

	newTaskID, err := r.state.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.state.ReadOr(instructionSurfaceBaselineStateKey, ""); got != oldBaseline {
		t.Fatalf("fresh-task cleanup removed task-ID-bound instruction baseline: %q", got)
	}
	if _, err := r.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatalf("new task did not rebind instruction baseline: %v", err)
	}
	baseline, err := r.state.Read(instructionSurfaceBaselineStateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(baseline, newTaskID+" ") {
		t.Fatalf("instruction baseline = %q want task %s", baseline, newTaskID)
	}
}
