//go:build !unix

package repolock

import (
	"os"
	"time"
)

type Lock struct {
	path string
}

func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrRepoLockHeld
	}
	file.Close()
	return &Lock{path: path}, nil
}

func AcquireWait(path string) (*Lock, error) {
	for {
		lock, err := Acquire(path)
		if err == nil {
			return lock, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
