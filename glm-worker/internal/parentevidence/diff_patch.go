package parentevidence

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func capturedDiffPatch(repoRoot string, file DiffFile, content []byte, present bool) (string, error) {
	dir, err := os.MkdirTemp("", "parent-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	before := os.DevNull
	if file.HeadBlob != "" {
		head, err := gitLiteralOutputIn(repoRoot, "cat-file", "blob", file.HeadBlob)
		if err != nil {
			return "", err
		}
		before, err = writeDiffSnapshot(dir, "a", file.Path, file.HeadMode, head)
		if err != nil {
			return "", err
		}
	}
	after := os.DevNull
	if present {
		after, err = writeDiffSnapshot(dir, "b", file.Path, file.WorktreeMode, content)
		if err != nil {
			return "", err
		}
	}
	command := exec.Command("git", "diff", "--no-index", "--no-prefix", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--", before, after)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	patch, err := command.Output()
	var exitError *exec.ExitError
	if err != nil && (!errors.As(err, &exitError) || exitError.ExitCode() != 1) {
		return "", fmt.Errorf("render captured diff for %q: %w: %s", file.Path, err, stderr.String())
	}
	return string(patch), nil
}

func writeDiffSnapshot(dir, side, path, mode string, content []byte) (string, error) {
	name := filepath.Join(side, filepath.FromSlash(path))
	destination := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	bits, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return "", fmt.Errorf("invalid captured mode %q: %w", mode, err)
	}
	switch bits & 0o170000 {
	case 0o120000:
		err = os.Symlink(string(content), destination)
	case 0o100000:
		perm := os.FileMode(0o600)
		if bits&0o111 != 0 {
			perm = 0o700
		}
		err = os.WriteFile(destination, content, perm)
	default:
		return "", fmt.Errorf("unsupported captured mode %q", mode)
	}
	return filepath.ToSlash(name), err
}

func diffLineCounts(patch string) (int, int) {
	additions, deletions := 0, 0
	hunk := false
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			hunk = false
		case strings.HasPrefix(line, "@@ "):
			hunk = true
		case hunk && strings.HasPrefix(line, "+"):
			additions++
		case hunk && strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}
