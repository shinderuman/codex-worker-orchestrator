//go:build unix

package installlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const lockDirectoryAnchor = "keep"

func Directory() (string, error) {
	uid := os.Geteuid()
	dir := filepath.Join("/tmp", fmt.Sprintf("codex-worker-orchestrator-install-%d", uid))
	if err := prepareLockDirectory(dir, uid); err != nil {
		return "", err
	}
	return dir, nil
}

func prepareLockDirectory(dir string, uid int) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create install lifecycle lock directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedBy(info, uid) {
		return fmt.Errorf("install lifecycle lock directory is not private to the current user: %s", dir)
	}
	return ensureLockDirectoryAnchor(dir, uid)
}

func ensureLockDirectoryAnchor(dir string, uid int) error {
	path := filepath.Join(dir, lockDirectoryAnchor)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		return file.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create install lifecycle lock anchor: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedBy(info, uid) {
		return fmt.Errorf("install lifecycle lock anchor is not private to the current user: %s", path)
	}
	return nil
}

func ownedBy(info os.FileInfo, uid int) bool {
	identity, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(identity.Uid) == uid
}

func Acquire(file *os.File) error {
	dir, err := Directory()
	if err != nil {
		return err
	}
	expected, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	actual, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(expected, actual) {
		return fmt.Errorf("install lock descriptor does not reference the private lock directory")
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrBusy
	}
	return err
}
