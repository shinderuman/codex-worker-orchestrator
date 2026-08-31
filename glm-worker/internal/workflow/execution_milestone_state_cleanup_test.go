package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStartNewTaskClearsExecutionMilestoneState(t *testing.T) {
	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "milestone-reset", RepoRoot: t.TempDir()}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(executionMilestoneStateFile, `{"version":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(executionMilestoneStateFile) {
		t.Fatal("execution milestone state survived StartNewTask")
	}
}
