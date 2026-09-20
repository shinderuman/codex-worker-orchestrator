package app

import (
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffIncludesPublicationSequenceSection(t *testing.T) {
	cfg := config.AppConfig{
		RepoRoot:  t.TempDir(),
		RepoHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StateBase: filepath.Join(t.TempDir(), "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Version != parentHandoffVersion {
		t.Fatalf("handoff version = %d", output.Version)
	}
	if output.Publication == nil {
		t.Fatalf("handoff lacks publication section: %#v", output)
	}
	if output.Publication.Stage != "blocked" || output.Publication.Failure == nil ||
		output.Publication.Failure.Reason != "publication_guard_setup_invalid" {
		t.Fatalf("publication section = %#v", output.Publication)
	}

	output.Consistent = false
	markHandoffInconsistent(&output, "probe")
	if output.Publication != nil {
		t.Fatalf("inconsistent handoff kept publication section: %#v", output.Publication)
	}
}
