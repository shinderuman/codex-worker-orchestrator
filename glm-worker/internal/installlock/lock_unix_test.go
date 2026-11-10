//go:build unix

package installlock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLockDirectoryResistsEmptyDirectoryCleanup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lock")
	if err := prepareLockDirectory(dir, os.Geteuid()); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); !errors.Is(err, syscall.ENOTEMPTY) {
		t.Fatalf("ロック保持中のディレクトリを削除できました: %v", err)
	}
	if err := os.Mkdir(dir, 0o700); !errors.Is(err, os.ErrExist) {
		t.Fatalf("ロック保持中のパスを再作成できました: %v", err)
	}
}

func TestLockDirectoryRejectsSymlinkAnchor(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("owned by the user"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, lockDirectoryAnchor)); err != nil {
		t.Fatal(err)
	}
	if err := prepareLockDirectory(dir, os.Geteuid()); err == nil {
		t.Fatal("symlinkをロックの保護ファイルとして受理しました")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "owned by the user" {
		t.Fatalf("symlinkの参照先が変化しました: %q, %v", data, err)
	}
}
