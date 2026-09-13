package taskdiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestChangedPathsDoesNotTreatFailedHeadBaselineAsUnborn(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	refsHeads := filepath.Join(repoRoot, ".git", "refs", "heads")
	if err := os.WriteFile(filepath.Join(refsHeads, "broken"), []byte("feedfacefeedfacefeedfacefeedfacefeedface\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".git", "HEAD"), []byte("ref: refs/heads/broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  strings.Repeat("e", 64),
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err == nil {
		t.Fatal("broken HEAD authority was captured as a baseline")
	}

	paths, available, err := ChangedPaths(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if available || len(paths) != 0 {
		t.Fatalf("failed baseline became an unborn baseline: available=%v paths=%v", available, paths)
	}
}
