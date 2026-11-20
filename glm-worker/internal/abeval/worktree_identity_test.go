package abeval

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeIdentityIndependentAliasAndDeleted(t *testing.T) {
	root := t.TempDir()
	directPath := filepath.Join(root, "direct")
	otherPath := filepath.Join(root, "other")
	aliasPath := filepath.Join(root, "alias")
	if err := os.Mkdir(directPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runABWorktreeGit(t, directPath, "init", "-q")
	runABWorktreeGit(t, directPath, "commit", "-q", "--allow-empty", "-m", "base")
	runABWorktreeGit(t, directPath, "worktree", "add", "-q", "--detach", otherPath, "HEAD")
	if err := os.Symlink(directPath, aliasPath); err != nil {
		t.Fatal(err)
	}
	capture := func(path string) WorktreeIdentity {
		t.Helper()
		id, err := CaptureWorktreeIdentity(path)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	spec := validSpec()
	direct, other := validDirectRecord(spec), validOrchestratedRecord(spec)
	direct.WorktreeIdentity = capture(directPath)
	direct.WorktreePath = direct.WorktreeIdentity.ResolvedPath
	other.WorktreeIdentity = capture(otherPath)
	other.WorktreePath = other.WorktreeIdentity.ResolvedPath
	if err := ValidatePair(spec, direct, other); err != nil {
		t.Fatalf("別worktree: %v", err)
	}
	other.WorktreeIdentity = capture(aliasPath)
	other.WorktreePath = other.WorktreeIdentity.ResolvedPath
	if err := ValidatePair(spec, direct, other); err == nil || !strings.Contains(err.Error(), "同一working tree identity") {
		t.Fatalf("aliasを独立と判定: %v", err)
	}
	other.WorktreeIdentity = capture(otherPath)
	other.WorktreePath = other.WorktreeIdentity.ResolvedPath
	if err := os.RemoveAll(otherPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(directPath); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePair(spec, direct, other); err != nil {
		t.Fatalf("削除済み別worktree: %v", err)
	}
	other.WorktreePath = direct.WorktreePath
	other.WorktreeIdentity = direct.WorktreeIdentity
	if err := ValidatePair(spec, direct, other); err == nil {
		t.Fatal("削除済み同一worktreeが受理されました")
	}
	other.WorktreePath = filepath.Join(root, "unrelated-deleted-worktree")
	if err := ValidatePair(spec, direct, other); err == nil {
		t.Fatal("削除済みworktreeのpathと採取時のpathの不一致が受理されました")
	}
}

func TestWorktreeIdentityRejectsMissingAndStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	spec := validSpec()
	direct, other := validDirectRecord(spec), validOrchestratedRecord(spec)
	runABWorktreeGit(t, path, "init", "-q")
	id, err := CaptureWorktreeIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	direct.WorktreePath, direct.WorktreeIdentity = id.ResolvedPath, id
	if err := ValidatePair(spec, direct, other); err != nil {
		t.Fatalf("valid identity: %v", err)
	}
	direct.WorktreeIdentity = WorktreeIdentity{}
	if err := ValidatePair(spec, direct, other); err == nil {
		t.Fatal("identityなしが受理されました")
	}
	direct.WorktreeIdentity = id
	direct.WorktreeIdentity.Inode++
	if err := ValidatePair(spec, direct, other); err == nil || !strings.Contains(err.Error(), "現在のworktreeと一致しません") {
		t.Fatalf("不一致のidentityが受理されました: %v", err)
	}
	direct.WorktreeIdentity = id
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	runABWorktreeGit(t, path, "init", "-q")
	if err := ValidatePair(spec, direct, other); err == nil {
		t.Fatal("再作成後のstale identityが受理されました")
	}
}

func TestCaptureWorktreeIdentityRejectsInvalidPath(t *testing.T) {
	root := t.TempDir()
	if _, err := CaptureWorktreeIdentity(filepath.Join(root, "missing")); err == nil {
		t.Fatal("不存在worktreeで採取成功")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureWorktreeIdentity(file); err == nil {
		t.Fatal("fileで採取成功")
	}
	if _, err := CaptureWorktreeIdentity(root); err == nil {
		t.Fatal("Git管理外のdirectoryで採取成功")
	}
	runABWorktreeGit(t, root, "init", "-q")
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureWorktreeIdentity(nested); err == nil {
		t.Fatal("同一worktree内の別directoryで独立したidentityの採取成功")
	}
}

func runABWorktreeGit(t *testing.T, root string, args ...string) {
	t.Helper()
	base := []string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false"}
	if output, err := exec.Command("git", append(base, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
