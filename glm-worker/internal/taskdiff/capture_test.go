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
	writeFile(t, filepath.Join(repoRoot, "blob.bin"), "\x00\x01binary\n")
	runGit(t, repoRoot, "add", "tracked.txt", "blob.bin")
	runGit(t, repoRoot, "commit", "-qm", "baseline")
	writeFile(t, filepath.Join(repoRoot, "baseline-preexisting.txt"), "preexisting\n")
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
	if err := st.Write("baseline-status", "?? baseline-preexisting.txt\n"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"baseline-index.patch", "baseline-worktree.patch"} {
		writeFile(t, st.Path(name), "")
	}
	writeFile(t, st.Path("baseline-untracked"), "baseline-preexisting.txt\x00")

	writeFile(t, filepath.Join(repoRoot, "tracked.txt"), "after\n")
	if err := os.Remove(filepath.Join(repoRoot, "blob.bin")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repoRoot, "new.txt"), "new\n")
	writeFile(t, filepath.Join(repoRoot, "committed-during-task.txt"), "committed\n")
	writeFile(t, filepath.Join(repoRoot, "binary-during-task.bin"), "\x00\x03binary new\n")
	runGit(t, repoRoot, "add", "committed-during-task.txt", "binary-during-task.bin")
	runGit(t, repoRoot, "commit", "-qm", "task change")
	writeFile(t, filepath.Join(repoRoot, "committed-then-removed.txt"), "removed\n")
	runGit(t, repoRoot, "add", "committed-then-removed.txt")
	runGit(t, repoRoot, "commit", "-qm", "task removal")
	if err := os.Remove(filepath.Join(repoRoot, "committed-then-removed.txt")); err != nil {
		t.Fatal(err)
	}

	diff, available, err := Capture(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("baseline unexpectedly unavailable")
	}
	for _, want := range [][]byte{
		[]byte("+after"),
		[]byte("new.txt"),
		[]byte("+new"),
		[]byte("new file mode"),
		[]byte("committed-during-task.txt"),
		[]byte("+committed"),
		[]byte("binary-during-task.bin"),
		[]byte("blob.bin"),
		[]byte("deleted file mode"),
	} {
		if !bytes.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
	if got := bytes.Count(diff, []byte("GIT binary patch")); got != 2 {
		t.Fatalf("binary patch count = %d, want 2:\n%s", got, diff)
	}
	for _, unwanted := range []string{"baseline-preexisting.txt", "committed-then-removed.txt"} {
		if bytes.Contains(diff, []byte(unwanted)) {
			t.Fatalf("diff contains %q:\n%s", unwanted, diff)
		}
	}
	if st.Exists("review-current-task.patch") {
		t.Fatal("Capture mutated reviewer state")
	}
}

func TestCaptureKeepsTaskCreatedFileAcrossCommit(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoRoot, "init", "-q")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repoRoot, "seed.txt"), "seed\n")
	runGit(t, repoRoot, "add", "seed.txt")
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

	writeFile(t, filepath.Join(repoRoot, "created-during-task.txt"), "created\n")
	if err := os.Symlink("/nonexistent/task-target", filepath.Join(repoRoot, "dangling-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("seed.txt", filepath.Join(repoRoot, "file-link")); err != nil {
		t.Fatal(err)
	}
	beforeCommit, available, err := Capture(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("baseline unexpectedly unavailable")
	}
	runGit(t, repoRoot, "add", "created-during-task.txt", "dangling-link", "file-link")
	runGit(t, repoRoot, "commit", "-qm", "task change")
	afterCommit, available, err := Capture(repoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("baseline unexpectedly unavailable")
	}
	for _, diff := range [][]byte{beforeCommit, afterCommit} {
		for _, want := range [][]byte{
			[]byte("new file mode"),
			[]byte("created-during-task.txt"),
			[]byte("+created"),
			[]byte("new file mode 120000"),
			[]byte("dangling-link"),
			[]byte("+/nonexistent/task-target"),
			[]byte("file-link"),
			[]byte("+seed.txt"),
		} {
			if !bytes.Contains(diff, want) {
				t.Fatalf("diff missing %q:\n%s", want, diff)
			}
		}
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
