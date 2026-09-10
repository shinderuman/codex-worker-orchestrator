package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestResultCorrectionStateIsClearedAtTaskBoundaries(t *testing.T) {
	st, err := NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "result-correction-state",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(ResultCorrectionStateFile, "stale"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(ResultCorrectionStateFile) {
		t.Fatal("new task transition left stale result correction state")
	}

	if err := st.Write(ResultCorrectionStateFile, "stale"); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(ResultCorrectionStateFile) {
		t.Fatal("reset left stale result correction state")
	}
}
