package workflow

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeCleanTaskBaselineState(t *testing.T, st *state.StateStore, baseline string) {
	t.Helper()
	if err := st.Write("baseline-head", baseline); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-status", "clean"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"baseline-index.patch", "baseline-worktree.patch", "baseline-untracked"} {
		if err := os.WriteFile(st.Path(name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkflowChangedPathsLayerConservativeExpansionOnExactTaskPaths(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "changed-paths@example.invalid")
	runGitTest(t, root, "config", "user.name", "changed paths")
	writeGitTestFile(t, root, "seed.go", "package sample\n")
	runGitTest(t, root, "add", ".")
	runGitTest(t, root, "commit", "-m", "baseline")

	writeGitTestFile(t, root, "seed.go", "package sample\n\nvar preTask = true\n")
	writeGitTestFile(t, root, "pre-task-untracked.txt", "pre-task\n")
	cfg := config.AppConfig{RepoRoot: root, StateBase: t.TempDir(), RepoHash: "workflow-changed-paths"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	writeGitTestFile(t, root, "task-created.txt", "task\n")

	exact, err := collectExactTaskChangedPaths(root, st)
	if err != nil {
		t.Fatal(err)
	}
	exactSet := changedPathSet(exact)
	if !exactSet["task-created.txt"] || exactSet["seed.go"] || exactSet["pre-task-untracked.txt"] {
		t.Fatalf("exact task paths = %v", exact)
	}

	conservative, err := collectTaskChangedPaths(root, st)
	if err != nil {
		t.Fatal(err)
	}
	conservativeSet := changedPathSet(conservative)
	for _, want := range []string{"seed.go", "pre-task-untracked.txt", "task-created.txt"} {
		if !conservativeSet[want] {
			t.Fatalf("conservative workflow paths missing %s: %v", want, conservative)
		}
	}
}

func changedPathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[path] = true
	}
	return result
}
