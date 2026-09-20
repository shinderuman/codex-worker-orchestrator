package app

import (
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffDoesNotProjectPublicationWhenInconsistent(t *testing.T) {
	cfg := config.AppConfig{
		RepoRoot:  t.TempDir(),
		RepoHash:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		StateBase: filepath.Join(t.TempDir(), "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("repo-root", ""); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Consistent {
		t.Fatalf("handoff unexpectedly consistent: %#v", output)
	}
	if output.Publication != nil {
		t.Fatalf("inconsistent handoff exposed publication action: %#v", output.Publication)
	}
}
