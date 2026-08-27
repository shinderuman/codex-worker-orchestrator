package runner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const instructionSurfaceBaselineStateKey = "instruction-surface-baseline-v1"

type instructionSurfaceEntry struct {
	path       string
	mode       fs.FileMode
	content    []byte
	linkTarget string
}

type instructionSurfaceSnapshot struct {
	entries []instructionSurfaceEntry
	digest  string
}

type InstructionSurfaceGuardError struct {
	Stage        string
	ChangedPaths []string
	Restored     bool
	Cause        error
}

func (e *InstructionSurfaceGuardError) Error() string {
	parts := []string{"repository instruction surface guard failed", e.Stage}
	if len(e.ChangedPaths) > 0 {
		parts = append(parts, strings.Join(e.ChangedPaths, ","))
	}
	if e.Restored {
		parts = append(parts, "restored")
	}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *InstructionSurfaceGuardError) Unwrap() error {
	return e.Cause
}

func (r *ClaudeRunner) prepareInstructionSurfaceGuard() (instructionSurfaceSnapshot, error) {
	current, err := captureInstructionSurfaceSnapshot(r.config.RepoRoot)
	if err != nil {
		return instructionSurfaceSnapshot{}, &InstructionSurfaceGuardError{Stage: "capture-before-call", Cause: err}
	}
	if !r.state.Exists(instructionSurfaceBaselineStateKey) {
		if err := r.state.Write(instructionSurfaceBaselineStateKey, current.digest); err != nil {
			return instructionSurfaceSnapshot{}, &InstructionSurfaceGuardError{Stage: "persist-task-baseline", Cause: err}
		}
		return current, nil
	}
	baseline, err := r.state.Read(instructionSurfaceBaselineStateKey)
	if err != nil {
		return instructionSurfaceSnapshot{}, &InstructionSurfaceGuardError{Stage: "read-task-baseline", Cause: err}
	}
	if strings.TrimSpace(baseline) != current.digest {
		return instructionSurfaceSnapshot{}, &InstructionSurfaceGuardError{
			Stage:        "before-call-mismatch",
			ChangedPaths: []string{"AGENTS.md/AGENTS.local.md"},
		}
	}
	return current, nil
}

func (r *ClaudeRunner) verifyInstructionSurfaceGuard(before instructionSurfaceSnapshot) error {
	after, err := captureInstructionSurfaceSnapshot(r.config.RepoRoot)
	if err != nil {
		return &InstructionSurfaceGuardError{Stage: "capture-after-call", Cause: err}
	}
	if before.digest == after.digest {
		return nil
	}
	changed := instructionSurfaceChangedPaths(before, after)
	if err := restoreInstructionSurface(r.config.RepoRoot, before, after); err != nil {
		return &InstructionSurfaceGuardError{Stage: "restore-after-call", ChangedPaths: changed, Cause: err}
	}
	restored, err := captureInstructionSurfaceSnapshot(r.config.RepoRoot)
	if err != nil {
		return &InstructionSurfaceGuardError{Stage: "verify-restored", ChangedPaths: changed, Cause: err}
	}
	if restored.digest != before.digest {
		return &InstructionSurfaceGuardError{
			Stage:        "verify-restored",
			ChangedPaths: changed,
			Cause:        fmt.Errorf("restored instruction surface does not match the pre-call snapshot"),
		}
	}
	return &InstructionSurfaceGuardError{Stage: "after-call-mutation", ChangedPaths: changed, Restored: true}
}

func captureInstructionSurfaceSnapshot(root string) (instructionSurfaceSnapshot, error) {
	if strings.TrimSpace(root) == "" {
		return instructionSurfaceSnapshot{}, fmt.Errorf("repository root is empty")
	}
	entries := make([]instructionSurfaceEntry, 0, 4)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isRepositoryInstructionName(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item, err := readInstructionSurfaceEntry(path, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return instructionSurfaceSnapshot{}, fmt.Errorf("enumerate repository instruction surface: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return instructionSurfaceSnapshot{entries: entries, digest: instructionSurfaceDigest(entries)}, nil
}

func isRepositoryInstructionName(name string) bool {
	return name == "AGENTS.md" || name == "AGENTS.local.md"
}

func readInstructionSurfaceEntry(absolute string, relative string) (instructionSurfaceEntry, error) {
	info, err := os.Lstat(absolute)
	if err != nil {
		return instructionSurfaceEntry{}, fmt.Errorf("inspect instruction surface %s: %w", relative, err)
	}
	item := instructionSurfaceEntry{path: relative, mode: info.Mode()}
	switch {
	case info.Mode().IsRegular():
		item.content, err = os.ReadFile(absolute)
		if err != nil {
			return instructionSurfaceEntry{}, fmt.Errorf("read instruction surface %s: %w", relative, err)
		}
	case info.Mode()&os.ModeSymlink != 0:
		item.linkTarget, err = os.Readlink(absolute)
		if err != nil {
			return instructionSurfaceEntry{}, fmt.Errorf("read instruction symlink %s: %w", relative, err)
		}
		item.content, err = os.ReadFile(absolute)
		if err != nil {
			return instructionSurfaceEntry{}, fmt.Errorf("read instruction symlink target %s: %w", relative, err)
		}
	default:
		return instructionSurfaceEntry{}, fmt.Errorf("instruction surface %s is not a regular file or symlink", relative)
	}
	return item, nil
}

func instructionSurfaceDigest(entries []instructionSurfaceEntry) string {
	hash := sha256.New()
	for _, item := range entries {
		_, _ = hash.Write([]byte(item.path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(item.mode.String()))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(item.linkTarget))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(item.content)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func instructionSurfaceChangedPaths(before instructionSurfaceSnapshot, after instructionSurfaceSnapshot) []string {
	beforeByPath := make(map[string]instructionSurfaceEntry, len(before.entries))
	afterByPath := make(map[string]instructionSurfaceEntry, len(after.entries))
	paths := make(map[string]struct{}, len(before.entries)+len(after.entries))
	for _, item := range before.entries {
		beforeByPath[item.path] = item
		paths[item.path] = struct{}{}
	}
	for _, item := range after.entries {
		afterByPath[item.path] = item
		paths[item.path] = struct{}{}
	}
	changed := make([]string, 0, len(paths))
	for path := range paths {
		left, leftOK := beforeByPath[path]
		right, rightOK := afterByPath[path]
		if leftOK != rightOK || !sameInstructionSurfaceEntry(left, right) {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func sameInstructionSurfaceEntry(left instructionSurfaceEntry, right instructionSurfaceEntry) bool {
	return left.path == right.path && left.mode == right.mode && left.linkTarget == right.linkTarget && bytes.Equal(left.content, right.content)
}

func restoreInstructionSurface(root string, before instructionSurfaceSnapshot, after instructionSurfaceSnapshot) error {
	beforeByPath := make(map[string]instructionSurfaceEntry, len(before.entries))
	for _, item := range before.entries {
		beforeByPath[item.path] = item
	}
	for _, item := range after.entries {
		if _, ok := beforeByPath[item.path]; ok {
			continue
		}
		if err := removeInstructionSurfacePath(root, item.path); err != nil {
			return err
		}
	}
	for _, item := range before.entries {
		if current, ok := instructionSurfaceEntryByPath(after.entries, item.path); ok && sameInstructionSurfaceEntry(item, current) {
			continue
		}
		if err := restoreInstructionSurfaceEntry(root, item); err != nil {
			return err
		}
	}
	return nil
}

func instructionSurfaceEntryByPath(entries []instructionSurfaceEntry, path string) (instructionSurfaceEntry, bool) {
	for _, item := range entries {
		if item.path == path {
			return item, true
		}
	}
	return instructionSurfaceEntry{}, false
}

func removeInstructionSurfacePath(root string, relative string) error {
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect changed instruction surface %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("changed instruction surface %s is not safely removable", relative)
	}
	if err := os.Remove(absolute); err != nil {
		return fmt.Errorf("remove changed instruction surface %s: %w", relative, err)
	}
	return nil
}

func restoreInstructionSurfaceEntry(root string, item instructionSurfaceEntry) error {
	absolute := filepath.Join(root, filepath.FromSlash(item.path))
	if err := ensureInstructionSurfaceParent(root, item.path); err != nil {
		return err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("instruction surface %s was replaced by a non-file path", item.path)
		}
		if err := os.Remove(absolute); err != nil {
			return fmt.Errorf("remove modified instruction surface %s: %w", item.path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect modified instruction surface %s: %w", item.path, err)
	}
	if item.mode&os.ModeSymlink != 0 {
		if err := os.Symlink(item.linkTarget, absolute); err != nil {
			return fmt.Errorf("restore instruction symlink %s: %w", item.path, err)
		}
		return nil
	}
	if err := os.WriteFile(absolute, item.content, item.mode.Perm()); err != nil {
		return fmt.Errorf("restore instruction surface %s: %w", item.path, err)
	}
	if err := os.Chmod(absolute, item.mode.Perm()); err != nil {
		return fmt.Errorf("restore instruction mode %s: %w", item.path, err)
	}
	return nil
}

func ensureInstructionSurfaceParent(root string, relative string) error {
	parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(relative)))
	relParent, err := filepath.Rel(root, parent)
	if err != nil {
		return err
	}
	current := root
	if relParent == "." {
		return nil
	}
	for _, part := range strings.Split(relParent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		switch {
		case err == nil && info.IsDir():
			continue
		case err == nil:
			return fmt.Errorf("instruction surface parent %s is not a directory", current)
		case os.IsNotExist(err):
			if err := os.Mkdir(current, 0o755); err != nil {
				return fmt.Errorf("restore instruction surface parent %s: %w", current, err)
			}
		default:
			return fmt.Errorf("inspect instruction surface parent %s: %w", current, err)
		}
	}
	return nil
}
