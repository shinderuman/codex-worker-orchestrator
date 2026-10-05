package controller

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type attemptRuntimeEvidence struct {
	store   *Store
	runtime *state.StateStore
	seal    *AttemptSeal
	taskID  string
}

func (s *Store) captureAttemptRuntimeEvidence(source Admission, seal *AttemptSeal) error {
	runtime, taskID, err := s.boundAttemptRuntime(source)
	if err != nil || runtime == nil {
		return err
	}
	collector := attemptRuntimeEvidence{store: s, runtime: runtime, seal: seal, taskID: taskID}
	if err := collector.captureStateFiles(); err != nil {
		return err
	}
	return collector.captureSessions()
}

func (s *Store) boundAttemptRuntime(source Admission) (*state.StateStore, string, error) {
	cfg := s.config
	cfg.RepoHash = digestStrings(s.identity.LineageID, source.Attempt.SemanticTaskRef.TaskPath)
	runtime := state.AttachStateStore(cfg)
	info, err := os.Lstat(runtime.Path("."))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", s.requireNoModelRuntimeEvidence(source.Attempt.AttemptID)
	}
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("runtime evidence source is not a directory")
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", s.requireNoModelRuntimeEvidence(source.Attempt.AttemptID)
	}
	if err != nil {
		return nil, "", fmt.Errorf("read runtime controller binding: %w", err)
	}
	if binding.AttemptID != source.Attempt.AttemptID {
		return nil, "", fmt.Errorf("runtime evidence belongs to another attempt")
	}
	if binding.TaskPath != source.Attempt.SemanticTaskRef.TaskPath || binding.TaskContractDigest != source.Attempt.SemanticTaskRef.ContractDigest {
		return nil, "", fmt.Errorf("runtime evidence semantic task binding is stale")
	}
	taskID, err := validateRuntimeEvidenceTask(runtime)
	return runtime, taskID, err
}

func validateRuntimeEvidenceTask(runtime *state.StateStore) (string, error) {
	taskID, err := runtime.TaskID()
	if err != nil {
		return "", err
	}
	if taskID == "" || filepath.Base(taskID) != taskID || strings.ContainsAny(taskID, `/\`) {
		return "", fmt.Errorf("runtime evidence task identity is invalid")
	}
	return taskID, nil
}

func (c *attemptRuntimeEvidence) captureStateFiles() error {
	entries, err := os.ReadDir(c.runtime.Path("."))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "lock" || entry.IsDir() {
			continue
		}
		if err := c.addFile(c.runtime.Path(entry.Name()), "runtime-state", "state/"+entry.Name()); err != nil {
			return err
		}
	}
	files := []struct{ path, kind, logical string }{
		{c.runtime.ModelCallLogPath(c.taskID), "telemetry", "telemetry.jsonl"},
		{c.runtime.TaskEventLogPath(c.taskID), "task-events", "events.jsonl"},
		{c.runtime.TaskLiveStatusPath(c.taskID), "task-events", "live.json"},
		{c.runtime.RoundLogPath(c.taskID), "review-rounds", "rounds.jsonl"},
		{c.runtime.TaskLifecycleLogPath(c.taskID), "task-lifecycle", "lifecycle.jsonl"},
		{c.runtime.TaskAuthorityPathPath(c.taskID), "task-authority", "authority.path"},
		{c.runtime.TaskAuthorityContentPath(c.taskID), "task-authority", "authority.md"},
	}
	for _, file := range files {
		if _, err := os.Lstat(file.path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := c.addFile(file.path, file.kind, file.logical); err != nil {
			return err
		}
	}
	return c.captureArtifactTree()
}

func (c *attemptRuntimeEvidence) captureArtifactTree() error {
	root := c.runtime.ArtifactDir(c.taskID)
	if info, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("runtime artifact source is not a directory")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return c.addFile(path, "runtime-artifact", "artifacts/"+filepath.ToSlash(name))
	})
}

func (c *attemptRuntimeEvidence) addFile(path, kind, logical string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime evidence %s is not a regular file", logical)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read required runtime evidence %s: %w", logical, err)
	}
	ref, err := c.store.PutEvidenceObject(kind, "application/octet-stream", c.seal.AttemptID+":"+logical, true, data)
	if err != nil {
		return err
	}
	c.seal.EvidenceRefs = append(c.seal.EvidenceRefs, ref)
	return nil
}

func (s *Store) requireNoModelRuntimeEvidence(attemptID string) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "mutations"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		identity, canonical, err := canonicalJSONRecordEntry(entry)
		if err != nil {
			return err
		}
		if !canonical {
			continue
		}
		var mutation MutationRecord
		if err := readJSON(s.mutationPath(identity), &mutation); err != nil {
			return err
		}
		if mutation.AttemptID == attemptID && strings.HasPrefix(mutation.Command, "model:") {
			return fmt.Errorf("required model runtime evidence is unavailable for attempt %s", attemptID)
		}
	}
	return nil
}
