package workflow

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAcceptedFixScopeAllowsOnlyPreviouslyReviewedChangesToRemain(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "scope-test")
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar baseline = 1\n")
	writeScopeFile(t, repo, "IMPLEMENTATION_TASKS/task.md", "# Task\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")

	writeScopeFile(t, repo, "IMPLEMENTATION_TASKS/task.md", "# Task\n\n## Amendments\n\nparent update\n")
	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "scope-test"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar baseline = 1\nvar retained = 2\nvar presentation = 3\nvar deadMakeEntry = 4\n")

	w := NewWorkflow(cfg, st, nil, io.Discard)
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	if !st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("accepted fix scope was not captured")
	}

	writeScopeFile(t, repo, "code.go", "package sample\n\nvar baseline = 1\nvar retained = 2\n")
	if !w.acceptedFixScopeCoversCurrent() {
		t.Fatal("shrinking the reviewed change set should remain inside accepted scope")
	}

	writeScopeFile(t, repo, "code.go", "package sample\n\nvar baseline = 1\nvar retained = 2\nvar presentation = 3\nvar deadMakeEntry = 4\n")
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar baseline = 1\nvar retained = 2\nvar newSemanticChoice = 9\n")
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("new semantic change must not fit the accepted scope")
	}
}

func TestAcceptedFixScopeDisablesOptimizationForNonParentDirtyBaseline(t *testing.T) {
	repo := t.TempDir()
	gitScope(t, repo, "init")
	gitScope(t, repo, "config", "user.email", "scope@example.invalid")
	gitScope(t, repo, "config", "user.name", "scope-test")
	writeScopeFile(t, repo, "code.go", "package sample\n")
	gitScope(t, repo, "add", ".")
	gitScope(t, repo, "commit", "-m", "baseline")
	writeScopeFile(t, repo, "code.go", "package sample\n\nvar preexisting = 1\n")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "scope-dirty"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, io.Discard)
	w.prepareAcceptedFixScope(acceptedFixScopeCurrentDiff)
	if st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("non-parent dirty baseline must keep the existing Sol risk-floor path")
	}
}

func gitScope(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeScopeFile(t *testing.T, repo, path, content string) {
	t.Helper()
	absolute := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
