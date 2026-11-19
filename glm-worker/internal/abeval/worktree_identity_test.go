package abeval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeIdentityIndependentAliasAndDeleted(t *testing.T) {
	root := t.TempDir()
	directPath := filepath.Join(root, "direct")
	otherPath := filepath.Join(root, "other")
	aliasPath := filepath.Join(root, "alias")
	for _, path := range []string{directPath, otherPath} {
		if err := os.Mkdir(path, 0o700); err != nil { t.Fatal(err) }
	}
	if err := os.Symlink(directPath, aliasPath); err != nil { t.Fatal(err) }
	capture := func(path string) WorktreeIdentity {
		t.Helper()
		id, err := CaptureWorktreeIdentity(path)
		if err != nil { t.Fatal(err) }
		return id
	}
	spec := validSpec()
	direct, other := validDirectRecord(spec), validOrchestratedRecord(spec)
	direct.WorktreePath, direct.WorktreeIdentity = directPath, capture(directPath)
	other.WorktreePath, other.WorktreeIdentity = otherPath, capture(otherPath)
	if err := ValidatePair(spec, direct, other); err != nil { t.Fatalf("別worktree: %v", err) }
	other.WorktreePath, other.WorktreeIdentity = aliasPath, capture(aliasPath)
	if err := ValidatePair(spec, direct, other); err == nil || !strings.Contains(err.Error(), "同一working tree identity") {
		t.Fatalf("aliasを独立と判定: %v", err)
	}
	other.WorktreePath, other.WorktreeIdentity = otherPath, capture(otherPath)
	if err := os.RemoveAll(otherPath); err != nil { t.Fatal(err) }
	if err := os.RemoveAll(directPath); err != nil { t.Fatal(err) }
	if err := ValidatePair(spec, direct, other); err != nil { t.Fatalf("削除済み別worktree: %v", err) }
	other.WorktreeIdentity = direct.WorktreeIdentity
	if err := ValidatePair(spec, direct, other); err == nil { t.Fatal("削除済み同一worktreeが受理されました") }
}

func TestWorktreeIdentityRejectsMissingAndStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(path, 0o700); err != nil { t.Fatal(err) }
	spec := validSpec()
	direct, other := validDirectRecord(spec), validOrchestratedRecord(spec)
	direct.WorktreePath = path
	id, err := CaptureWorktreeIdentity(path)
	if err != nil { t.Fatal(err) }
	direct.WorktreeIdentity = id
	if err := ValidatePair(spec, direct, other); err != nil { t.Fatalf("valid identity: %v", err) }
	direct.WorktreeIdentity = WorktreeIdentity{}
	if err := ValidatePair(spec, direct, other); err == nil { t.Fatal("identityなしが受理されました") }
	direct.WorktreeIdentity = id
	direct.WorktreeIdentity.Inode++
	if err := ValidatePair(spec, direct, other); err == nil || !strings.Contains(err.Error(), "現在のworktreeと一致しません") {
		t.Fatalf("不一致のidentityが受理されました: %v", err)
	}
	direct.WorktreeIdentity = id
	if err := os.Rename(path, path+"-old"); err != nil { t.Fatal(err) }
	if err := os.Mkdir(path, 0o700); err != nil { t.Fatal(err) }
	if err := ValidatePair(spec, direct, other); err == nil { t.Fatal("再作成後のstale identityが受理されました") }
}

func TestCaptureWorktreeIdentityRejectsInvalidPath(t *testing.T) {
	root := t.TempDir()
	if _, err := CaptureWorktreeIdentity(filepath.Join(root, "missing")); err == nil { t.Fatal("不存在worktreeで採取成功") }
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil { t.Fatal(err) }
	if _, err := CaptureWorktreeIdentity(file); err == nil { t.Fatal("fileで採取成功") }
}
