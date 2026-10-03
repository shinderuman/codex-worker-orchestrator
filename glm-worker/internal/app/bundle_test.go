package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newBundleTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	initBundleGitRepository(t, repoRoot)
	cfg := config.AppConfig{
		RepoRoot:        repoRoot,
		RepoHash:        strings.Repeat("a", 64),
		RepoShort:       strings.Repeat("a", 12),
		StateBase:       filepath.Join(root, ".glm-worker", "sessions"),
		ClaudeConfigDir: filepath.Join(root, ".claude"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func initBundleGitRepository(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	writeBundleFile(t, filepath.Join(repoRoot, repositoryharness.MarkerPath), repositoryharness.MarkerContent)
	run("add", "--", repositoryharness.MarkerPath)
	run("commit", "-q", "-m", "base")
}

func writeBundleFile(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
