package parentevidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxDiffSourceBytes int64 = 1 << 20 // Independent of the requested display budget.
const maxDiffPatchBytes = 2 << 20 // Bounds each patch and the complete requested diff.

type boundedDiffOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (out *boundedDiffOutput) Write(data []byte) (int, error) {
	if len(data) > out.limit-out.Len() {
		out.exceeded = true
		return 0, fmt.Errorf("diff subprocess output exceeds %d-byte execution limit", out.limit)
	}
	return out.Buffer.Write(data)
}

// HEAD blobs are immutable, so checking their declared size prevents expensive
// extraction before the captured snapshot is constructed.
func readCapturedHeadBlob(repoRoot, blob string) ([]byte, error) {
	sizeBytes, err := gitLiteralOutputIn(repoRoot, "cat-file", "-s", blob)
	if err != nil {
		return nil, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeBytes)), 10, 64)
	if err != nil || size < 0 {
		return nil, fmt.Errorf("invalid size for HEAD blob %s: %q", blob, sizeBytes)
	}
	if size > maxDiffSourceBytes {
		return nil, fmt.Errorf("HEAD blob %s is %d bytes; source execution limit is %d bytes", blob, size, maxDiffSourceBytes)
	}
	content, err := gitLiteralOutputIn(repoRoot, "cat-file", "blob", blob)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxDiffSourceBytes {
		return nil, fmt.Errorf("HEAD blob %s exceeded source execution limit during read", blob)
	}
	return content, nil
}

func capturedDiffPatch(repoRoot string, file DiffFile, content []byte, present bool) (string, error) {
	dir, err := os.MkdirTemp("", "parent-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	before := os.DevNull
	if file.HeadBlob != "" {
		head, err := readCapturedHeadBlob(repoRoot, file.HeadBlob)
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-prefix", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", "--", before, after)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	patch := &boundedDiffOutput{limit: maxDiffPatchBytes}
	stderr := &boundedDiffOutput{limit: 8 * 1024}
	command.Stdout = patch
	command.Stderr = stderr
	err = command.Run()
	if patch.exceeded {
		return "", fmt.Errorf("captured diff for %q exceeds %d-byte patch execution limit (HEAD %s, index %s, worktree %s)", file.Path, maxDiffPatchBytes, file.HeadBlob, file.IndexBlob, file.WorktreeSHA)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("render captured diff for %q: %w", file.Path, ctx.Err())
	}
	var exitError *exec.ExitError
	if err != nil && (!errors.As(err, &exitError) || exitError.ExitCode() != 1) {
		return "", fmt.Errorf("render captured diff for %q: %w: %s", file.Path, err, stderr.String())
	}
	return patch.String(), nil
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
