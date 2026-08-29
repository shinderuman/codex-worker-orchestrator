package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRuntimeBuildSettingsFromGo(t *testing.T) {
	got := runtimeBuildSettingsFromGo([]debug.BuildSetting{
		{Key: "vcs.revision", Value: " abc123 "},
		{Key: "vcs.modified", Value: "true"},
	})
	if got.revision != "abc123" || got.modified == nil || !*got.modified {
		t.Fatalf("settings = %#v", got)
	}
	unknown := runtimeBuildSettingsFromGo([]debug.BuildSetting{{Key: "vcs.modified", Value: "unknown"}})
	if unknown.modified != nil {
		t.Fatalf("unknown modified = %#v", unknown.modified)
	}
}

func TestRuntimeBuildStatusRelationships(t *testing.T) {
	repo := newRuntimeBuildRepo(t)
	initialBranch := runtimeBuildGit(t, repo, "branch", "--show-current")
	first := runtimeBuildGit(t, repo, "rev-parse", "HEAD")
	writeRuntimeBuildFile(t, repo, "file.txt", "second\n")
	runtimeBuildGit(t, repo, "add", "file.txt")
	runtimeBuildGit(t, repo, "commit", "-q", "-m", "second")
	second := runtimeBuildGit(t, repo, "rev-parse", "HEAD")

	clean := false
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{revision: second, modified: &clean}); got.Relationship != runtimeBuildSame || got.RepositoryHead == nil || *got.RepositoryHead != second {
		t.Fatalf("same = %#v", got)
	}
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{revision: first, modified: &clean}); got.Relationship != runtimeBuildAncestor {
		t.Fatalf("ancestor = %#v", got)
	}

	runtimeBuildGit(t, repo, "checkout", "-q", "-b", "side", first)
	writeRuntimeBuildFile(t, repo, "side.txt", "side\n")
	runtimeBuildGit(t, repo, "add", "side.txt")
	runtimeBuildGit(t, repo, "commit", "-q", "-m", "side")
	side := runtimeBuildGit(t, repo, "rev-parse", "HEAD")
	runtimeBuildGit(t, repo, "checkout", "-q", initialBranch)
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{revision: side, modified: &clean}); got.Relationship != runtimeBuildNotAncestor {
		t.Fatalf("not ancestor = %#v", got)
	}
}

func TestRuntimeBuildStatusUnknownBoundaries(t *testing.T) {
	repo := newRuntimeBuildRepo(t)
	head := runtimeBuildGit(t, repo, "rev-parse", "HEAD")
	modified := true
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{revision: head, modified: &modified}); got.Relationship != runtimeBuildUnknown || got.VCSModified == nil || !*got.VCSModified {
		t.Fatalf("modified = %#v", got)
	}
	clean := false
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{modified: &clean}); got.Relationship != runtimeBuildUnknown || got.VCSRevision != nil {
		t.Fatalf("missing revision = %#v", got)
	}
	if got := runtimeBuildStatus(t.TempDir(), runtimeBuildSettings{revision: head, modified: &clean}); got.Relationship != runtimeBuildUnknown || got.RepositoryHead != nil {
		t.Fatalf("non git = %#v", got)
	}
	if got := runtimeBuildStatus(repo, runtimeBuildSettings{revision: "0123456789012345678901234567890123456789", modified: &clean}); got.Relationship != runtimeBuildUnknown {
		t.Fatalf("unknown revision = %#v", got)
	}
}

func TestBuildStatusOutputIncludesRuntimeBuild(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := stateStoreForRuntimeBuildTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	output := buildStatusOutput(st, "", nil, nil)
	if output.RuntimeBuild.Relationship == "" {
		t.Fatal("runtime build relationship is empty")
	}
}

func stateStoreForRuntimeBuildTest(cfg config.AppConfig) (*state.StateStore, error) {
	return state.NewStateStore(cfg)
}

func newRuntimeBuildRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runtimeBuildGit(t, "", "init", "-q", repo)
	runtimeBuildGit(t, repo, "config", "user.email", "runtime-build@example.invalid")
	runtimeBuildGit(t, repo, "config", "user.name", "runtime build")
	writeRuntimeBuildFile(t, repo, "file.txt", "first\n")
	runtimeBuildGit(t, repo, "add", "file.txt")
	runtimeBuildGit(t, repo, "commit", "-q", "-m", "first")
	return repo
}

func runtimeBuildGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := args
	if repo != "" {
		commandArgs = append([]string{"-C", repo}, args...)
	}
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", commandArgs, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeRuntimeBuildFile(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
