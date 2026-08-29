package taskdiff

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCaptureReturnsBaselineToCurrentDiffWithoutMutatingState(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repoRoot, "tracked.txt"), "before\n")
	runGit(t, repoRoot, "add", "tracked.txt")
	runGit(t, repoRoot, "commit", "-qm", "baseline")
	head := strings.TrimSpace(runGit(t, repoRoot, "rev-parse", "HEAD"))

	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  strings.Repeat("a", 64),
		StateBase: filepath.Join(root, ".glm-worker", "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-head", head); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("baseline-status", "clean"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"baseline-index.patch", "baseline-worktree.patch", "baseline-untracked"} {
		writeFile(t, st.Path(name), "")
	}

	writeFile(t, filepath.Join(repoRoot, "tracked.txt"), "after\n")
	writeFile(t, filepath.Join(repoRoot, "new.txt"), "new\n")

	diff, available, err := Capture(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("baseline unexpectedly unavailable")
	}
	for _, want := range [][]byte{[]byte("+after"), []byte("new.txt"), []byte("+new")} {
		if !bytes.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
	if st.Exists("review-current-task.patch") {
		t.Fatal("Capture mutated reviewer state")
	}
}

func runGit(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
