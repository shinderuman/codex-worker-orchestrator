package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func captureCleanupParentActions(repo string, worktree []workspaceTreeEntry) ([]workspaceTreeEntry, error) {
	root := filepath.Join(repo, parentaction.StageDirName)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return worktree, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cleanup parent action staging root is not a directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, entry := range worktree {
		known[entry.path] = true
	}
	for _, entry := range entries {
		path := filepath.Join(parentaction.StageDirName, entry.Name())
		if known[path] {
			continue
		}
		captured, err := captureCleanupParentAction(repo, path)
		if err != nil {
			return nil, err
		}
		worktree = append(worktree, captured)
	}
	return worktree, nil
}

func captureCleanupParentAction(repo, path string) (workspaceTreeEntry, error) {
	info, err := os.Lstat(filepath.Join(repo, path))
	if err != nil {
		return workspaceTreeEntry{}, err
	}
	if !info.Mode().IsRegular() {
		return workspaceTreeEntry{}, fmt.Errorf("cleanup parent action data is not a regular file: %s", path)
	}
	entry, exists, err := captureSuspensionEntry(repo, workspaceTreeEntry{path: filepath.ToSlash(path)})
	if err != nil {
		return entry, err
	}
	if !exists {
		return entry, fmt.Errorf("cleanup parent action data disappeared: %s", path)
	}
	return entry, nil
}
