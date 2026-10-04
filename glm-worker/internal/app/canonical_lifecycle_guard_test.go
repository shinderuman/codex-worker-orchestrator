package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestOrdinaryWorkflowModesUseCanonicalAdmission(t *testing.T) {
	cfg := newCanonicalGuardConfig(t, false)
	for _, mode := range []CommandMode{ModeNewTask, ModeResume, ModeDecision, ModeFix, ModeAccept, ModeApproveSurface} {
		if !retainedCanonicalWorkflowMode(mode) {
			t.Fatalf("ordinary workflow mode %d does not enter canonical admission", mode)
		}
	}
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatalf("explicit canonical activation failed: %v", err)
	}
}

func newCanonicalGuardConfig(t *testing.T, activate bool) config.AppConfig {
	t.Helper()
	t.Setenv("GLM_WORKER_PARENT_ACTION", "")
	repo := t.TempDir()
	runCanonicalGuardGit(t, repo, "init", "-q")
	runCanonicalGuardGit(t, repo, "config", "user.email", "app-guard@example.invalid")
	runCanonicalGuardGit(t, repo, "config", "user.name", "App Guard Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	runCanonicalGuardGit(t, repo, "add", ".")
	runCanonicalGuardGit(t, repo, "commit", "-q", "-m", "base")
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

func runCanonicalGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
