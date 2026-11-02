package parentevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	parent, err := diffOpenParent(repoRoot, path)
	if os.IsNotExist(err) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, err
	}
	defer func() { _ = parent.Close() }()
	leaf := path[strings.LastIndex(path, "/")+1:]
	anchored := fmt.Sprintf("/proc/self/fd/%d/%s", parent.Fd(), leaf)
	info, err := os.Lstat(anchored)
	if os.IsNotExist(err) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, fmt.Errorf("inspect diff leaf %q: %w", path, err)
	}
	content, err := diffReadLeaf(parent, leaf, anchored, info)
	if err != nil {
		return "", "", nil, false, err
	}
	after, err := os.Lstat(anchored)
	if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() ||
		info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", "", nil, false, fmt.Errorf("diff leaf %q changed during capture", path)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), info.Mode().String(), content, true, nil
}

func diffOpenParent(repoRoot, path string) (*os.File, error) {
	parent, err := os.Open(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("open diff root: %w", err)
	}
	parts := strings.Split(path, "/")
	for _, component := range parts[:len(parts)-1] {
		fd, openErr := syscall.Openat(int(parent.Fd()), component,
			syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
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

func diffReadLeaf(parent *os.File, leaf, anchored string, info os.FileInfo) ([]byte, error) {
	switch {
	case info.Mode().IsRegular():
		return diffReadRegularLeaf(parent, leaf, info)
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(anchored)
		if err != nil {
			return nil, fmt.Errorf("read diff symlink %q: %w", leaf, err)
		}
		return []byte(link), nil
	default:
		return nil, fmt.Errorf("unsupported diff worktree type for %q: %s", leaf, info.Mode())
	}
}

func diffReadRegularLeaf(parent *os.File, leaf string, info os.FileInfo) ([]byte, error) {
	fd, err := syscall.Openat(int(parent.Fd()), leaf,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open diff leaf %q: %w", leaf, err)
	}
	file := os.NewFile(uintptr(fd), leaf)
	opened, statErr := file.Stat()
	if statErr == nil && (!opened.Mode().IsRegular() || !os.SameFile(info, opened)) {
		statErr = fmt.Errorf("diff leaf %q changed while opening", leaf)
	}
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read diff leaf %q: %w", leaf, readErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return content, nil
}
