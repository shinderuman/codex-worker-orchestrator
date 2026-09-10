//go:build unix

package app

import (
	"fmt"
	"os"
	"syscall"
)

type repoLockLease struct {
	file *os.File
}

func AcquireRepoLockLease(path string) (*repoLockLease, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("repo lock lease %sを開けません: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, ErrRepoLockHeld
	}
	return &repoLockLease{file: file}, nil
}

func (l *repoLockLease) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}
