package parentevidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newDiffIdentityRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitDiffIdentity(t, repo, "init", "-q")
	gitDiffIdentity(t, repo, "config", "user.email", "diff-test@example.invalid")
	gitDiffIdentity(t, repo, "config", "user.name", "Diff Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dir", "child.txt"), []byte("child before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDiffIdentity(t, repo, "add", ".")
	gitDiffIdentity(t, repo, "commit", "-qm", "baseline")
	return repo
}

func gitDiffIdentity(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func captureDiffIdentity(t *testing.T, root string, paths ...string) string {
	t.Helper()
	files, patch, err := captureDiff(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := Digest(struct {
		Question string
		Paths    []string
		Files    []DiffFile
		Patch    string
	}{"review", paths, files, patch})
	return digest
}

func TestDiffIdentityModeOnlyChangeAndUnchangedRequest(t *testing.T) {
	root := newDiffIdentityRepo(t)
	initial := captureDiffIdentity(t, root, "file.txt")
	if captureDiffIdentity(t, root, "file.txt") != initial {
		t.Fatal("unchanged diff has unstable identity")
	}
	if err := os.Chmod(filepath.Join(root, "file.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if next := captureDiffIdentity(t, root, "file.txt"); next == initial {
		t.Fatal("mode-only change did not change parent diff identity")
	}
}

func TestDiffIdentityChangedContentAndSymlinkTarget(t *testing.T) {
	root := newDiffIdentityRepo(t)
	initial := captureDiffIdentity(t, root, "file.txt")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := captureDiffIdentity(t, root, "file.txt"); after == initial {
		t.Fatal("content change did not change diff identity")
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink("file.txt", link); err != nil {
		t.Fatal(err)
	}
	linkInitial := captureDiffIdentity(t, root, "link.txt")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir/child.txt", link); err != nil {
		t.Fatal(err)
	}
	if changed := captureDiffIdentity(t, root, "link.txt"); changed == linkInitial {
		t.Fatal("symlink target change did not change identity")
	}
}

func TestDiffIdentityRejectsDirectoryAndPathspec(t *testing.T) {
	root := newDiffIdentityRepo(t)
	for _, path := range []string{"dir", "dir/", "file*.txt", ":(glob)*", "../file.txt"} {
		if _, _, err := captureDiff(root, []string{path}); err == nil {
			t.Fatalf("diff path %q was accepted without exact file identity", path)
		}
	}
}

func TestDiffIdentityUntrackedFileChangeAndDelete(t *testing.T) {
	root := newDiffIdentityRepo(t)
	target := filepath.Join(root, "new.txt")
	if err := os.WriteFile(target, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := captureDiffIdentity(t, root, "new.txt")
	if err := os.WriteFile(target, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := captureDiffIdentity(t, root, "new.txt"); first == second {
		t.Fatal("untracked content change was treated as delivered")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if deleted := captureDiffIdentity(t, root, "new.txt"); deleted == first {
		t.Fatal("removed untracked content had stale identity")
	}
}

func TestDiffIdentityRejectsGitPathspecSpecialSyntax(t *testing.T) {
	root := newDiffIdentityRepo(t)
	for _, path := range []string{":(exclude)file.txt", "file?.txt", "dir/../file.txt"} {
		if err := validateExactDiffPath(root, path); err == nil || !strings.Contains(err.Error(), "exact repository-relative") {
			t.Fatalf("path %q: %v", path, err)
		}
	}
}
