package workflow

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestFreshTaskCleanupOwnsWorkflowTaskStateBeforeInitializationCompletes(t *testing.T) {
	if qualitySurfaceBaselineStateKey != state.QualitySurfaceBaselineStateFile {
		t.Fatalf("quality surface state key is not registered in task lifetime policy: %q", qualitySurfaceBaselineStateKey)
	}
	if repositoryharness.ActivationStateKey != state.RepositoryHarnessActivationStateFile {
		t.Fatalf("repository harness state key is not registered in task lifetime policy: %q", repositoryharness.ActivationStateKey)
	}

	cfg := config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "new-task-state-lifetime",
		RepoRoot:  filepath.Join(t.TempDir(), "missing-repository"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "old-quality"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.ParentActionCodexThreadIDEnv, "")
	t.Setenv(state.ParentActionCodexSessionIDEnv, "")
	t.Setenv(state.SessionRotationClaimIDEnv, "")

	w := NewWorkflow(cfg, st, &scriptedRunner{}, io.Discard)
	if _, err := w.initializeNewTask("new request"); err == nil {
		t.Fatal("initializeNewTask unexpectedly succeeded with missing repository")
	}
	newTaskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if newTaskID == oldTaskID {
		t.Fatalf("new task identity was not committed: %s", newTaskID)
	}
	for _, name := range []string{qualitySurfaceBaselineStateKey, repositoryharness.ActivationStateKey} {
		if st.Exists(name) {
			t.Fatalf("prior task state remained authoritative after initialization failure: %s", name)
		}
	}
}
