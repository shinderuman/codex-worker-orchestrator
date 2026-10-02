package controller

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	var result []workspaceTreeEntry
	for path, entry := range paths {
		if parentManagedPath(path) {
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
	return result, nil
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
	mode := "100644"
	if info.Mode().Perm()&0o111 != 0 {
		mode = "100755"
	}
	data, err := os.ReadFile(path)
	return data, mode, err
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
