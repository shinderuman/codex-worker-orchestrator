package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestLegacyPublicationMutationBindingsAreRejectedAfterCutover(t *testing.T) {
	mutating := []string{
		publicationPrepareSubcommand,
		publicationInstallCandidateSubcommand,
		publicationPromoteSubcommand,
		publicationRecoverSubcommand,
	}
	for _, activate := range []bool{false, true} {
		cfg := newCanonicalCutoverConfig(t, activate)
		for _, subcommand := range mutating {
			err := rejectLegacyPublicationBinding(cfg, subcommand)
			if err == nil || !strings.Contains(err.Error(), "legacy publication binding "+subcommand+" is unavailable after canonical controller cutover") {
				t.Fatalf("activate=%v publication binding %s error = %v", activate, subcommand, err)
			}
		}
	}
}

func TestPublicationReadOnlyBindingsRemainAvailableAfterCutover(t *testing.T) {
	for _, activate := range []bool{false, true} {
		cfg := newCanonicalCutoverConfig(t, activate)
		for _, subcommand := range []string{publicationReadinessSubcommand, publicationRefGuardSubcommand, publicationPushGuardSubcommand} {
			if err := rejectLegacyPublicationBinding(cfg, subcommand); err != nil {
				t.Fatalf("activate=%v read-only publication binding %s was rejected: %v", activate, subcommand, err)
			}
		}
	}
}

func TestLegacyPublicationFindingRegistrationIsRejectedAfterCutover(t *testing.T) {
	for _, activate := range []bool{false, true} {
		cfg := newCanonicalCutoverConfig(t, activate)
		err := executeParentLifecycleAction(cfg, []string{actionRecordPublicationFinding, "--origin", "codex-review"}, nil)
		if err == nil || !strings.Contains(err.Error(), "legacy publication finding registration is unavailable after canonical controller cutover") {
			t.Fatalf("activate=%v record-publication-finding error = %v", activate, err)
		}
	}
}

func newCanonicalCutoverConfig(t *testing.T, activate bool) config.AppConfig {
	t.Helper()
	repo := t.TempDir()
	runCanonicalCutoverGit(t, repo, "init", "-q")
	runCanonicalCutoverGit(t, repo, "config", "user.email", "cutover@example.invalid")
	runCanonicalCutoverGit(t, repo, "config", "user.name", "Cutover Test")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md"), []byte("# root\n\n## Contract\n\nroot\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCanonicalCutoverGit(t, repo, "add", ".")
	runCanonicalCutoverGit(t, repo, "commit", "-q", "-m", "base")
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

func runCanonicalCutoverGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func canonicalCutoverWorktrees(t *testing.T, repo string) []string {
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
