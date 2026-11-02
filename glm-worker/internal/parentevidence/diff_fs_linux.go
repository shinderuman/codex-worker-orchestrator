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
	parent, err := os.Open(repoRoot)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("open diff root: %w", err)
	}
	defer parent.Close()
	components := strings.Split(path, "/")
	for _, component := range components[:len(components)-1] {
		fd, openErr := syscall.Openat(int(parent.Fd()), component,
			syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if os.IsNotExist(openErr) {
			return "", "", nil, false, nil
		}
		if openErr != nil {
			return "", "", nil, false, fmt.Errorf("reject diff parent %q: %w", component, openErr)
		}
		next := os.NewFile(uintptr(fd), component)
		defer next.Close()
		parent = next
	}
	leaf := components[len(components)-1]
	anchored := fmt.Sprintf("/proc/self/fd/%d/%s", parent.Fd(), leaf)
	info, err := os.Lstat(anchored)
	if os.IsNotExist(err) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, fmt.Errorf("inspect diff leaf %q: %w", path, err)
	}
	mode := info.Mode().String()
	var content []byte
	switch {
	case info.Mode().IsRegular():
		fd, openErr := syscall.Openat(int(parent.Fd()), leaf,
			syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if openErr != nil {
			return "", "", nil, false, fmt.Errorf("open diff leaf %q: %w", path, openErr)
		}
		file := os.NewFile(uintptr(fd), path)
		opened, statErr := file.Stat()
		if statErr == nil && (!opened.Mode().IsRegular() || !os.SameFile(info, opened)) {
			statErr = fmt.Errorf("diff leaf changed while opening")
		}
		if statErr == nil {
			content, statErr = io.ReadAll(file)
		}
		closeErr := file.Close()
		if statErr != nil {
			return "", "", nil, false, fmt.Errorf("read diff leaf %q: %w", path, statErr)
		}
		if closeErr != nil {
			return "", "", nil, false, closeErr
		}
	case info.Mode()&os.ModeSymlink != 0:
		link, readErr := os.Readlink(anchored)
		if readErr != nil {
			return "", "", nil, false, fmt.Errorf("read diff link %q: %w", path, readErr)
		}
		content = []byte(link)
	default:
		return "", "", nil, false, fmt.Errorf("unsupported diff worktree type for %q: %s", path, mode)
	}
	after, err := os.Lstat(anchored)
	if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() ||
		info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", "", nil, false, fmt.Errorf("diff leaf %q changed during capture", path)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), mode, content, true, nil
}
