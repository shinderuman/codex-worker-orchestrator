package parentactioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type finalizationRemoteFixture struct {
	repo    string
	branch  string
	baseOID string
}

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

func newPushBindingFixture(t *testing.T) finalizationRemoteFixture {
	t.Helper()
	repo := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "finalization@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "Finalization Test")
	runFinalizationGit(t, remote, "init", "-q", "--bare")
	writePushBindingFile(t, repo, "binding.txt", "base\n")
	runFinalizationGit(t, repo, "add", "binding.txt")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	branchOutput, err := gitFinalizationOutput(repo, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(branchOutput)
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "origin", branch)
	runFinalizationGit(t, repo, "branch", "--set-upstream-to=origin/"+branch, branch)
	baseOID, err := gitFinalizationOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return finalizationRemoteFixture{
		repo:    repo,
		branch:  branch,
		baseOID: strings.TrimSpace(baseOID),
	}
}

func writePushBindingFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
