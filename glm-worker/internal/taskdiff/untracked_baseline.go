package taskdiff

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func loadPreexistingUntracked(st *state.StateStore) ([]state.UntrackedBaselineEntry, map[string]state.UntrackedBaselineEntry, error) {
	if !st.Exists("baseline-untracked") {
		return nil, nil, fmt.Errorf("captured untracked baseline is unavailable")
	}
	entries, err := st.ReadUntrackedBaseline()
	if err != nil {
		return nil, nil, fmt.Errorf("read captured untracked baseline: %w", err)
	}
	byPath := make(map[string]state.UntrackedBaselineEntry, len(entries))
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	return entries, byPath, nil
}

func baselineUntrackedPathSet(entries []state.UntrackedBaselineEntry) map[string]struct{} {
	result := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		result[entry.Path] = struct{}{}
	}
	return result
}

func currentTrackedPathSet(repoRoot string) (map[string]struct{}, error) {
	raw, stderr, err := runGitCommand(repoRoot, nil, nil, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("list current tracked files: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	return nulPathSet(raw), nil
}

func preexistingUntrackedChangedPaths(
	repoRoot string,
	entries []state.UntrackedBaselineEntry,
) ([]string, error) {
	tracked, err := currentTrackedPathSet(repoRoot)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, entry := range entries {
		if _, nowTracked := tracked[entry.Path]; nowTracked {
			changed = append(changed, entry.Path)
			continue
		}
		same, err := preexistingUntrackedMatchesCurrent(repoRoot, entry)
		if err != nil {
			return nil, err
		}
		if !same {
			changed = append(changed, entry.Path)
		}
	}
	return changed, nil
}

func appendPreexistingUntrackedDiff(
	repoRoot string,
	st *state.StateStore,
	diff []byte,
) ([]byte, error) {
	entries, _, err := loadPreexistingUntracked(st)
	if err != nil {
		return nil, err
	}
	tracked, err := currentTrackedPathSet(repoRoot)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	result.Write(diff)
	for _, entry := range entries {
		_, nowTracked := tracked[entry.Path]
		patch, differs, err := preexistingUntrackedPathPatch(repoRoot, entry, nowTracked)
		if err != nil {
			return nil, err
		}
		if differs {
			result.Write(patch)
		}
	}
	return result.Bytes(), nil
}

func preexistingUntrackedPathPatch(
	repoRoot string,
	entry state.UntrackedBaselineEntry,
	nowTracked bool,
) ([]byte, bool, error) {
	if nowTracked {
		patch, err := trackedPreexistingUntrackedPatch(repoRoot, entry.Path)
		return patch, true, err
	}
	same, err := preexistingUntrackedMatchesCurrent(repoRoot, entry)
	if err != nil {
		return nil, false, err
	}
	if same {
		return nil, false, nil
	}
	patch, err := diffPreexistingUntrackedPath(repoRoot, entry)
	if err != nil {
		return nil, false, err
	}
	return patch, true, nil
}

func trackedPreexistingUntrackedPatch(repoRoot, path string) ([]byte, error) {
	if worktreePathPresent(repoRoot, path) {
		return newFilePatch(repoRoot, path)
	}
	args := []string{"diff", "--cached", "--binary", "--no-ext-diff", "--no-renames", "--", path}
	stdout, stderr, err := runGitCommand(repoRoot, nil, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("capture tracked pre-existing untracked path %s: %w: %s", path, err, strings.TrimSpace(string(stderr)))
	}
	if len(stdout) == 0 {
		return nil, fmt.Errorf("tracked pre-existing untracked path %s has no reviewable index evidence", path)
	}
	return stdout, nil
}

func preexistingUntrackedMatchesCurrent(repoRoot string, entry state.UntrackedBaselineEntry) (bool, error) {
	path := filepath.Join(repoRoot, filepath.FromSlash(entry.Path))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat current pre-existing untracked path %s: %w", entry.Path, err)
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		if entry.Kind != state.UntrackedBaselineKindSymlink {
			return false, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return false, fmt.Errorf("read current pre-existing untracked symlink %s: %w", entry.Path, err)
		}
		return bytes.Equal([]byte(target), entry.Content), nil
	case info.Mode().IsRegular():
		if entry.Kind != state.UntrackedBaselineKindFile || gitExecutable(info.Mode().Perm()) != gitExecutable(os.FileMode(entry.Mode)) {
			return false, nil
		}
		if info.Size() != entry.Size {
			return false, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("read current pre-existing untracked file %s: %w", entry.Path, err)
		}
		digest := sha256.Sum256(data)
		return fmt.Sprintf("%x", digest[:]) == entry.SHA256, nil
	default:
		return false, nil
	}
}

func diffPreexistingUntrackedPath(repoRoot string, entry state.UntrackedBaselineEntry) ([]byte, error) {
	tempDir, err := os.MkdirTemp(os.Getenv("GLM_WORKER_GIT_TEMP_ROOT"), "glm-worker-untracked-baseline-")
	if err != nil {
		return nil, fmt.Errorf("create pre-existing untracked diff temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	oldPath := filepath.Join(tempDir, "a", filepath.FromSlash(entry.Path))
	if err := materializeUntrackedBaselineEntry(oldPath, entry); err != nil {
		return nil, err
	}
	newRoot := filepath.Join(tempDir, "b")
	if err := os.Symlink(repoRoot, newRoot); err != nil {
		return nil, fmt.Errorf("link current worktree for pre-existing untracked diff: %w", err)
	}
	currentPath := filepath.Join(repoRoot, filepath.FromSlash(entry.Path))
	newArg := filepath.ToSlash(filepath.Join("b", filepath.FromSlash(entry.Path)))
	if info, err := os.Lstat(currentPath); errors.Is(err, os.ErrNotExist) {
		newArg = "/dev/null"
	} else if err != nil {
		return nil, fmt.Errorf("stat current pre-existing untracked path %s: %w", entry.Path, err)
	} else if info.IsDir() || (!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0) {
		return nil, fmt.Errorf("current pre-existing untracked path %s has unsupported file type", entry.Path)
	}

	oldArg := filepath.ToSlash(filepath.Join("a", filepath.FromSlash(entry.Path)))
	args := []string{"diff", "--no-index", "--binary", "--no-ext-diff", "--no-renames", "--no-prefix", "--", oldArg, newArg}
	stdout, stderr, err := runGitCommand(tempDir, nil, nil, args...)
	if err == nil {
		return stdout, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return stdout, nil
	}
	return nil, fmt.Errorf("capture pre-existing untracked diff for %s: %w: %s", entry.Path, err, strings.TrimSpace(string(stderr)))
}

func materializeUntrackedBaselineEntry(path string, entry state.UntrackedBaselineEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create pre-existing untracked baseline parent: %w", err)
	}
	switch entry.Kind {
	case state.UntrackedBaselineKindFile:
		if err := os.WriteFile(path, entry.Content, os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("materialize pre-existing untracked file %s: %w", entry.Path, err)
		}
		if err := os.Chmod(path, os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("restore pre-existing untracked mode %s: %w", entry.Path, err)
		}
	case state.UntrackedBaselineKindSymlink:
		if err := os.Symlink(string(entry.Content), path); err != nil {
			return fmt.Errorf("materialize pre-existing untracked symlink %s: %w", entry.Path, err)
		}
	default:
		return fmt.Errorf("unsupported pre-existing untracked kind %q for %s", entry.Kind, entry.Path)
	}
	return nil
}

func gitExecutable(mode os.FileMode) bool {
	return mode&0o111 != 0
}
