package controller

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func captureSuspensionWorktree(repo string, indexed []workspaceTreeEntry) ([]workspaceTreeEntry, error) {
	paths := make(map[string]workspaceTreeEntry, len(indexed))
	for _, entry := range indexed {
		paths[entry.path] = entry
	}
	other, err := runGitBinary(repo, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range strings.Split(string(other), "\x00") {
		if path != "" {
			paths[path] = workspaceTreeEntry{path: path}
		}
	}
	var entries []workspaceTreeEntry
	for path, entry := range paths {
		if !state.IsParentManagedPath(path) {
			entries = append(entries, entry)
		}
	}
	return captureSuspensionEntries(repo, entries)
}

func captureSuspensionEntries(repo string, entries []workspaceTreeEntry) ([]workspaceTreeEntry, error) {
	var result, regular []workspaceTreeEntry
	for _, entry := range entries {
		path, err := suspensionWorktreePath(repo, entry.path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() && entry.mode != "160000" {
			entry.mode = suspensionRegularMode(info.Mode())
			regular = append(regular, entry)
			continue
		}
		captured, exists, err := captureSuspensionEntry(repo, entry)
		if err != nil {
			return nil, err
		}
		if exists {
			result = append(result, captured)
		}
	}
	regular, err := hashSuspensionFiles(repo, regular)
	return append(result, regular...), err
}

func hashSuspensionFiles(repo string, entries []workspaceTreeEntry) ([]workspaceTreeEntry, error) {
	for start := 0; start < len(entries); start += 128 {
		end := min(start+128, len(entries))
		args := []string{"hash-object", "--no-filters", "-w", "--"}
		for _, entry := range entries[start:end] {
			args = append(args, entry.path)
		}
		data, err := runGitBinary(repo, nil, args...)
		if err != nil {
			return nil, err
		}
		objects := strings.Fields(string(data))
		if len(objects) != end-start {
			return nil, fmt.Errorf("suspension blob batch returned unexpected object count")
		}
		for index, oid := range objects {
			entries[start+index].oid = oid
		}
	}
	return entries, nil
}

func captureSuspensionEntry(repo string, indexed workspaceTreeEntry) (workspaceTreeEntry, bool, error) {
	path, err := suspensionWorktreePath(repo, indexed.path)
	if err != nil {
		return workspaceTreeEntry{}, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return workspaceTreeEntry{}, false, nil
	}
	if err != nil {
		return workspaceTreeEntry{}, false, err
	}
	if indexed.mode == "160000" {
		return captureSuspensionGitlink(path, indexed)
	}
	if info.IsDir() {
		return workspaceTreeEntry{}, false, nil
	}
	data, mode, err := suspensionFileBytes(path, info)
	if err != nil {
		return workspaceTreeEntry{}, false, err
	}
	oid, err := runGitBinary(repo, data, "hash-object", "-w", "--stdin")
	if err != nil {
		return workspaceTreeEntry{}, false, err
	}
	return workspaceTreeEntry{path: indexed.path, mode: mode, oid: strings.TrimSpace(string(oid))}, true, nil
}

func suspensionFileBytes(path string, info os.FileInfo) ([]byte, string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), "120000", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("unsupported suspension filesystem object %q", path)
	}
	data, err := os.ReadFile(path)
	return data, suspensionRegularMode(info.Mode()), err
}

func suspensionRegularMode(mode os.FileMode) string {
	if mode.Perm()&0o111 != 0 {
		return "100755"
	}
	return "100644"
}

func captureSuspensionGitlink(path string, entry workspaceTreeEntry) (workspaceTreeEntry, bool, error) {
	if _, err := os.Lstat(path + string(os.PathSeparator) + ".git"); os.IsNotExist(err) {
		files, err := os.ReadDir(path)
		if err != nil {
			return workspaceTreeEntry{}, false, err
		}
		if len(files) != 0 {
			return workspaceTreeEntry{}, false, fmt.Errorf("uninitialized submodule has workspace content")
		}
		return entry, true, nil
	}
	head, err := gitTrimmed(path, "rev-parse", "HEAD")
	if err != nil {
		return workspaceTreeEntry{}, false, fmt.Errorf("unreadable suspension submodule %q: %w", path, err)
	}
	dirty, err := runGitBinary(path, nil, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return workspaceTreeEntry{}, false, err
	}
	if len(dirty) != 0 {
		return workspaceTreeEntry{}, false, fmt.Errorf("dirty suspension submodule %q", path)
	}
	if head != entry.oid {
		return workspaceTreeEntry{}, false, fmt.Errorf("unstaged submodule commit is not reproducible in a detached lane")
	}
	entry.oid = head
	return entry, true, nil
}
