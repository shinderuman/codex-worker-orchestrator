//go:build linux || darwin

package controller

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"golang.org/x/sys/unix"
)

func openValidationRunDirectory(rootPath, runID string) (*os.File, error) {
	rootFD, err := unix.Open(rootPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(rootFD), rootPath)
	for _, component := range []string{qualitygate.RunDirectory, runID} {
		fd, openErr := unix.Openat(int(parent.Fd()), component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			_ = parent.Close()
			return nil, openErr
		}
		next := os.NewFile(uintptr(fd), component)
		if err := parent.Close(); err != nil {
			_ = next.Close()
			return nil, err
		}
		parent = next
	}
	return parent, nil
}

func openValidationRunLeaf(parent *os.File, filename string) (*os.File, unix.Stat_t, error) {
	var before unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), filename, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, before, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, before, fmt.Errorf("validation run source is not a regular file")
	}
	fd, err := unix.Openat(int(parent.Fd()), filename, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, before, err
	}
	return os.NewFile(uintptr(fd), filename), before, nil
}

func readValidationFileNoFollow(rootPath, runID, filename string, read func(*os.File) ([]byte, error)) ([]byte, error) {
	parent, err := openValidationRunDirectory(rootPath, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	file, before, err := openValidationRunLeaf(parent, filename)
	if err != nil {
		return nil, err
	}
	var opened unix.Stat_t
	statErr := unix.Fstat(int(file.Fd()), &opened)
	openedInfo, infoErr := file.Stat()
	var data []byte
	var readErr error
	if statErr == nil && infoErr == nil && sameValidationFileStat(before, opened) {
		data, readErr = read(file)
	} else {
		readErr = fmt.Errorf("validation run source changed while opening")
	}
	afterInfo, afterInfoErr := file.Stat()
	closeErr := file.Close()
	var after unix.Stat_t
	afterErr := unix.Fstatat(int(parent.Fd()), filename, &after, unix.AT_SYMLINK_NOFOLLOW)
	if err := errors.Join(statErr, infoErr, readErr, afterInfoErr, closeErr, afterErr); err != nil {
		return nil, err
	}
	if !sameValidationFileStat(before, opened) || !sameValidationFileStat(opened, after) ||
		evidenceExportFileChanged(openedInfo, afterInfo) {
		return nil, fmt.Errorf("validation run source changed during read")
	}
	return data, nil
}

func sameValidationFileStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Size == b.Size
}
