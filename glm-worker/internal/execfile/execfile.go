package execfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	temp, err := WriteStream(dir, ".execfile-*", bytes.NewReader(data), perm)
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return errors.Join(err, os.Remove(temp))
	}
	return nil
}

func WriteStream(dir, pattern string, source io.Reader, perm os.FileMode) (string, error) {
	syscall.ForkLock.RLock()
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return "", err
	}
	path := file.Name()
	_, writeErr := io.Copy(file, source)
	chmodErr := file.Chmod(perm)
	closeErr := file.Close()
	syscall.ForkLock.RUnlock()
	if err := errors.Join(writeErr, chmodErr, closeErr); err != nil {
		return "", errors.Join(err, os.Remove(path))
	}
	return path, nil
}
