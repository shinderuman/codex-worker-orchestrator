//go:build linux || darwin

package parentevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	parent, err := diffOpenParent(repoRoot, path)
	if errors.Is(err, unix.ENOENT) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, err
	}
	defer func() { _ = parent.Close() }()
	leaf := path[strings.LastIndex(path, "/")+1:]
	var info unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", "", nil, false, nil
		}
		return "", "", nil, false, fmt.Errorf("inspect diff leaf %q: %w", path, err)
	}
	content, err := diffReadLeaf(parent, leaf, info)
	if err != nil {
		return "", "", nil, false, err
	}
	var after unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameDiffObject(info, after) {
		return "", "", nil, false, fmt.Errorf("diff leaf %q changed during capture: %w", path, err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), fmt.Sprintf("%06o", info.Mode), content, true, nil
}

func diffOpenParent(repoRoot, path string) (*os.File, error) {
	parent, err := os.Open(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("open diff root: %w", err)
	}
	parts := strings.Split(path, "/")
	for _, component := range parts[:len(parts)-1] {
		fd, openErr := unix.Openat(int(parent.Fd()), component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			_ = parent.Close()
			return nil, fmt.Errorf("reject diff parent %q: %w", component, openErr)
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

func diffReadLeaf(parent *os.File, leaf string, info unix.Stat_t) ([]byte, error) {
	switch info.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		return diffReadRegularLeaf(parent, leaf, info)
	case unix.S_IFLNK:
		for size := 256; ; size *= 2 {
			buffer := make([]byte, size)
			n, err := unix.Readlinkat(int(parent.Fd()), leaf, buffer)
			if err != nil {
				return nil, fmt.Errorf("read diff symlink %q: %w", leaf, err)
			}
			if n < len(buffer) {
				return buffer[:n], nil
			}
		}
	default:
		return nil, fmt.Errorf("unsupported diff worktree type for %q: %o", leaf, info.Mode)
	}
}

func diffReadRegularLeaf(parent *os.File, leaf string, info unix.Stat_t) ([]byte, error) {
	fd, err := unix.Openat(int(parent.Fd()), leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open diff leaf %q: %w", leaf, err)
	}
	file := os.NewFile(uintptr(fd), leaf)
	defer func() { _ = file.Close() }()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return nil, err
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFREG || !sameDiffObject(info, opened) {
		return nil, fmt.Errorf("diff leaf %q changed while opening", leaf)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read diff leaf %q: %w", leaf, err)
	}
	return content, nil
}

func sameDiffObject(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Size == after.Size && before.Mtim == after.Mtim
}
