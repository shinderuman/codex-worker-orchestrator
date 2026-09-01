package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func IsParentManagedPath(path string) bool {
	for _, name := range parentManagedFiles {
		if path == name {
			return true
		}
	}
	return strings.HasPrefix(path, ParentTasksDir+"/")
}

func CaptureParentFileState(repoRoot, name string) (ParentFileState, error) {
	content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(name)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ParentFileState{Path: name}, nil
		}
		return ParentFileState{}, fmt.Errorf("read %s: %w", name, err)
	}
	sum := sha256.Sum256(content)
	return ParentFileState{Path: name, Exists: true, SHA256: hex.EncodeToString(sum[:])}, nil
}

func CaptureParentFileStates(repoRoot string) (ParentFileStates, error) {
	states := make(ParentFileStates, 0, len(parentManagedFiles)+8)
	for _, name := range parentManagedFiles {
		state, err := CaptureParentFileState(repoRoot, name)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	tasks, err := captureParentTaskFileStates(repoRoot)
	if err != nil {
		return nil, err
	}
	states = append(states, tasks...)
	sort.Slice(states, func(i, j int) bool { return states[i].Path < states[j].Path })
	return states, nil
}

func captureParentTaskFileStates(repoRoot string) (ParentFileStates, error) {
	dir := filepath.Join(repoRoot, ParentTasksDir)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", ParentTasksDir, err)
	}
	var states ParentFileStates
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		state, err := CaptureParentFileState(repoRoot, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		states = append(states, state)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate %s: %w", ParentTasksDir, err)
	}
	return states, nil
}

func CaptureRepositoryBoundarySnapshot(repoRoot string) (GitSnapshot, error) {
	snapshot, err := CaptureGitSnapshot(repoRoot)
	if err != nil {
		return GitSnapshot{}, err
	}
	parentFiles, err := CaptureParentFileStates(repoRoot)
	if err != nil {
		return GitSnapshot{}, err
	}
	snapshot.ParentFiles = &parentFiles
	return snapshot, nil
}
