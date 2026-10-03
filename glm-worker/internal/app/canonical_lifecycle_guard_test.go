package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestLegacyStateLifecycleIsRejectedAfterCanonicalCutover(t *testing.T) {
	for _, activate := range []bool{false, true} {
		cfg := newCanonicalGuardConfig(t, activate)
		modes := []struct {
			mode    CommandMode
			command string
		}{
			{mode: ModeReset, command: "reset"},
			{mode: ModePark, command: "park"},
			{mode: ModeUnpark, command: "unpark"},
			{mode: ModeIsolate, command: "isolate"},
		}
		for _, tc := range modes {
			err := Execute(Command{Mode: tc.mode, Payload: "request"}, cfg, nil, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "legacy "+tc.command+" lifecycle is unavailable after canonical controller cutover") {
				t.Fatalf("activate=%t %s legacy path error = %v", activate, tc.command, err)
			}
		}
		if worktrees := canonicalGuardWorktrees(t, cfg.RepoRoot); len(worktrees) != 0 {
			t.Fatalf("activate=%t legacy lifecycle rejection left derived worktrees behind: %v", activate, worktrees)
		}
		if branches := canonicalGuardBranches(t, cfg.RepoRoot); len(branches) != 0 {
			t.Fatalf("activate=%t legacy lifecycle rejection left derived branches behind: %v", activate, branches)
		}
	}
}

func TestOrdinaryWorkflowModesUseCanonicalAdmission(t *testing.T) {
	cfg := newCanonicalGuardConfig(t, false)
	if active, err := controller.CanonicalAuthorityActive(cfg); err != nil || active {
		t.Fatalf("pristine fixture reported canonical authority: active=%v err=%v", active, err)
	}
	for _, mode := range []CommandMode{ModeNewTask, ModeResume, ModeDecision, ModeFix, ModeAccept, ModeApproveSurface} {
		if !retainedCanonicalWorkflowMode(mode) || rejectLegacyStateLifecycle(cfg, mode) != nil {
			t.Fatalf("ordinary workflow mode %d does not enter canonical admission", mode)
		}
	}
	if _, err := controller.Activate(cfg); err != nil {
		t.Fatalf("explicit canonical activation failed: %v", err)
	}
	if active, err := controller.CanonicalAuthorityActive(cfg); err != nil || !active {
		t.Fatalf("explicit activation did not establish canonical authority: active=%v err=%v", active, err)
	}
}

func TestLegacyBundleProjectionIsAlwaysRetired(t *testing.T) {
	for _, activate := range []bool{false, true} {
		cfg := newCanonicalGuardConfig(t, activate)
		err := rejectLegacyBundleProjection(cfg)
		if err == nil || !strings.Contains(err.Error(), "controller-evidence") {
			t.Fatalf("activate=%t legacy bundle projection error = %v", activate, err)
		}
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

func canonicalGuardWorktrees(t *testing.T, repo string) []string {
	t.Helper()
	output, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	primary := ""
	for _, block := range strings.Split(strings.TrimSpace(string(output)), "\n\n") {
		fields := strings.Split(block, "\n")
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "worktree ") {
			continue
		}
		path := strings.TrimPrefix(fields[0], "worktree ")
		if primary == "" {
			primary = path
			continue
		}
		extra = append(extra, path)
	}
	return extra
}

func canonicalGuardBranches(t *testing.T, repo string) []string {
	t.Helper()
	output, err := exec.Command("git", "-C", repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/").Output()
	if err != nil {
		t.Fatal(err)
	}
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" || line == "master" || line == "main" {
			continue
		}
		branches = append(branches, line)
	}
	return branches
}
