package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitWorkspaceUsesExactCommitAndPreservesDirtyPrimary(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "commit lint test")
	git("config", "user.email", "commit-lint@example.invalid")
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("final source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "final source")
	final := git("rev-parse", "HEAD")
	if err := os.WriteFile(path, []byte("later source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "later source")
	if err := os.WriteFile(path, []byte("dirty primary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := git("status", "--porcelain=v1")
	workspace, err := newCommitWorkspace(root, final)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	content, err := os.ReadFile(filepath.Join(workspace, "source.txt"))
	if err != nil || string(content) != "final source\n" {
		t.Fatalf("commit lint used primary worktree instead of final commit: %q %v", content, err)
	}
	content, err = os.ReadFile(path)
	if err != nil || string(content) != "dirty primary\n" || git("status", "--porcelain=v1") != before {
		t.Fatal("commit lint changed primary source or Git state")
	}
	for _, invalid := range []string{"HEAD", strings.Repeat("0", len(final))} {
		if workspace, err := newCommitWorkspace(root, invalid); err == nil {
			_ = os.RemoveAll(workspace)
			t.Fatalf("invalid exact commit used another source: %s", invalid)
		}
	}
}
