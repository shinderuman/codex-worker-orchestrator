//go:build !unix

package repolock

import "os"

type Lock struct {
	path string
}

func Acquire(path string) (*Lock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrHeld
	}
	file.Close()
	return &Lock{path: path}, nil
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
