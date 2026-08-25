//go:build !unix

package app

import (
	"fmt"
	"os"
)

type RepoLock struct {
	path string
}

func AcquireRepoLock(path string) (*RepoLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrRepoLockHeld
	}
	file.Close()
	return &RepoLock{path: path}, nil
}

func (l *RepoLock) Close() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
