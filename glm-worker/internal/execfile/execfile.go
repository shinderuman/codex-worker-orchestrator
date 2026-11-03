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
	path, identity, err := createStreamFile(dir, pattern)
	if err != nil {
		return "", err
	}
	if err := writeStreamChunks(path, identity, source); err != nil {
		return "", errors.Join(err, removeStreamFile(path, identity))
	}
	if err := withStreamFile(path, identity, func(file *os.File) error { return file.Chmod(perm) }); err != nil {
		return "", errors.Join(err, removeStreamFile(path, identity))
	}
	return path, nil
}

func createStreamFile(dir, pattern string) (string, os.FileInfo, error) {
	syscall.ForkLock.RLock()
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return "", nil, err
	}
	path := file.Name()
	chmodErr := file.Chmod(0o600)
	identity, statErr := file.Stat()
	closeErr := file.Close()
	syscall.ForkLock.RUnlock()
	if err := errors.Join(chmodErr, statErr, closeErr); err != nil {
		return "", nil, errors.Join(err, os.Remove(path))
	}
	return path, identity, nil
}

func writeStreamChunks(path string, identity os.FileInfo, source io.Reader) error {
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := source.Read(buffer)
		if n > 0 {
			if err := withStreamFile(path, identity, func(file *os.File) error {
				written, err := file.Write(buffer[:n])
				if err == nil && written != n {
					return io.ErrShortWrite
				}
				return err
			}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func withStreamFile(path string, identity os.FileInfo, operation func(*os.File) error) (err error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	file, err := openStreamFile(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	current, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(identity, current) {
		return errors.New("実行fileの一時fileが置換されました")
	}
	return operation(file)
}

func removeStreamFile(path string, identity os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(identity, current) {
		return errors.New("実行fileの回収対象が置換されました")
	}
	return os.Remove(path)
}
