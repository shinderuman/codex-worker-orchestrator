//go:build go1.24

package parentevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("open diff root: %w", err)
	}
	defer root.Close()
	components := strings.Split(path, "/")
	for i := 1; i < len(components); i++ {
		prefix := strings.Join(components[:i], "/")
		info, statErr := root.Lstat(prefix)
		if os.IsNotExist(statErr) {
			return "", "", nil, false, nil
		}
		if statErr != nil {
			return "", "", nil, false, fmt.Errorf("inspect diff parent %q: %w", prefix, statErr)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", nil, false, fmt.Errorf("non-directory or symlink diff parent %q", prefix)
		}
	}
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, fmt.Errorf("inspect diff path %q: %w", path, err)
	}
	mode := info.Mode().String()
	var content []byte
	switch {
	case info.Mode().IsRegular():
		file, openErr := root.Open(path)
		if openErr != nil {
			return "", "", nil, false, fmt.Errorf("open diff path %q: %w", path, openErr)
		}
		opened, statErr := file.Stat()
		if statErr == nil && (!opened.Mode().IsRegular() || !os.SameFile(info, opened)) {
			statErr = fmt.Errorf("diff path changed during read")
		}
		if statErr == nil {
			content, statErr = io.ReadAll(file)
		}
		closeErr := file.Close()
		if statErr != nil {
			return "", "", nil, false, fmt.Errorf("read diff path %q: %w", path, statErr)
		}
		if closeErr != nil {
			return "", "", nil, false, closeErr
		}
	case info.Mode()&os.ModeSymlink != 0:
		link, readErr := root.Readlink(path)
		if readErr != nil {
			return "", "", nil, false, fmt.Errorf("read diff link %q: %w", path, readErr)
		}
		content = []byte(link)
	default:
		return "", "", nil, false, fmt.Errorf("unsupported diff worktree type for %q: %s", path, mode)
	}
	after, err := root.Lstat(path)
	if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() ||
		info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", "", nil, false, fmt.Errorf("diff path %q changed during capture", path)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), mode, content, true, nil
}
