package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func newCanonicalCutoverConfig(t *testing.T, activate bool) config.AppConfig {
	t.Helper()
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "controller-action@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "Controller Action Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "add", ".")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "base")
	hash := config.RepoHashFor(repo)
	cfg := config.AppConfig{
		RepoRoot:     repo,
		RepoHash:     hash,
		RepoShort:    hash[:12],
		StateBase:    filepath.Join(t.TempDir(), "sessions"),
		WorktreeBase: filepath.Join(t.TempDir(), "worktrees"),
	}
	if activate {
		if _, err := controller.Activate(cfg); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}
