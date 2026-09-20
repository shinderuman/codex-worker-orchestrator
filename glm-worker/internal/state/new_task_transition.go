package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

type newTaskTransitionFileSnapshot struct {
	data   []byte
	exists bool
}

type newTaskTransitionSnapshot map[string]newTaskTransitionFileSnapshot

func (s *StateStore) commitNewTaskCanonicalState(taskID string, afterCanonicalCommit func() error) error {
	lock, err := repolock.AcquireWait(s.Path(ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()

	additionalFiles, err := s.pendingSessionRotationRetirementTransitionFiles()
	if err != nil {
		return err
	}
	snapshot, err := s.captureNewTaskTransitionSnapshotForFiles(additionalFiles)
	if err != nil {
		return err
	}
	if err := s.validatePendingSessionRotationRecommendationRetirementBoundary(); err != nil {
		return err
	}
	rollback := func(cause error) error {
		rollbackErr := s.restoreNewTaskTransitionSnapshotForFiles(snapshot, additionalFiles)
		if rollbackErr != nil {
			return errors.Join(cause, fmt.Errorf("new task transitionをrollbackできません: %w", rollbackErr))
		}
		return cause
	}
	if err := s.applyNewTaskCanonicalState(taskID); err != nil {
		return rollback(err)
	}
	if err := s.commitPendingSessionRotationRecommendationRetirement(); err != nil {
		return rollback(err)
	}
	if afterCanonicalCommit != nil {
		if err := afterCanonicalCommit(); err != nil {
			return rollback(err)
		}
	}
	return nil
}

func (s *StateStore) applyNewTaskCanonicalState(taskID string) error {
	if err := s.Remove("task.id"); err != nil {
		return err
	}
	if err := s.InvalidateAllSessions(); err != nil {
		return err
	}
	if err := s.Remove(newTaskTransitionStateFileNames()...); err != nil {
		return err
	}
	if err := s.Remove(taskDispositionStateFile); err != nil {
		return err
	}
	if err := s.ClearParentEvidenceLedger(); err != nil {
		return err
	}
	if err := s.advanceParentEvidenceLeaseUnlocked(); err != nil {
		return err
	}
	if err := s.initializeParentReviewState(taskID); err != nil {
		return err
	}
	if err := s.Write("task.status", string(TaskStatusActive)); err != nil {
		return err
	}
	return s.Write("task.id", taskID)
}

func (s *StateStore) captureNewTaskTransitionSnapshot() (newTaskTransitionSnapshot, error) {
	return s.captureNewTaskTransitionSnapshotWithAdditional(nil)
}

func (s *StateStore) captureNewTaskTransitionSnapshotForFiles(additional []string) (newTaskTransitionSnapshot, error) {
	if len(additional) == 0 {
		return s.captureNewTaskTransitionSnapshot()
	}
	return s.captureNewTaskTransitionSnapshotWithAdditional(additional)
}

func (s *StateStore) captureNewTaskTransitionSnapshotWithAdditional(additional []string) (newTaskTransitionSnapshot, error) {
	snapshot := make(newTaskTransitionSnapshot)
	for _, name := range newTaskTransitionSnapshotFileNames(additional) {
		data, err := os.ReadFile(s.Path(name))
		if errors.Is(err, os.ErrNotExist) {
			snapshot[name] = newTaskTransitionFileSnapshot{}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("new task transition state %sを読めません: %w", name, err)
		}
		snapshot[name] = newTaskTransitionFileSnapshot{data: data, exists: true}
	}
	return snapshot, nil
}

func (s *StateStore) restoreNewTaskTransitionSnapshot(snapshot newTaskTransitionSnapshot) error {
	return s.restoreNewTaskTransitionSnapshotWithAdditional(snapshot, nil)
}

func (s *StateStore) restoreNewTaskTransitionSnapshotForFiles(snapshot newTaskTransitionSnapshot, additional []string) error {
	if len(additional) == 0 {
		return s.restoreNewTaskTransitionSnapshot(snapshot)
	}
	return s.restoreNewTaskTransitionSnapshotWithAdditional(snapshot, additional)
}

func (s *StateStore) restoreNewTaskTransitionSnapshotWithAdditional(snapshot newTaskTransitionSnapshot, additional []string) error {
	var result error
	for _, name := range newTaskTransitionSnapshotFileNames(additional) {
		if name == "task.id" {
			continue
		}
		result = errors.Join(result, s.restoreNewTaskTransitionFile(name, snapshot[name]))
	}
	if result != nil {
		return result
	}
	return s.restoreNewTaskTransitionFile("task.id", snapshot["task.id"])
}

func (s *StateStore) restoreNewTaskTransitionFile(name string, snapshot newTaskTransitionFileSnapshot) error {
	if !snapshot.exists {
		err := removeStatePath(s.Path(name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("state %sをrollback削除できません: %w", name, err)
		}
		return nil
	}
	if err := writeFileAtomic(s.Path(name), snapshot.data, 0o600); err != nil {
		return fmt.Errorf("state %sをrollback復元できません: %w", name, err)
	}
	return nil
}

func newTaskTransitionSnapshotFileNames(additional []string) []string {
	names := newTaskCanonicalStateFileNames()
	seen := make(map[string]struct{}, len(names)+len(additional))
	for _, name := range names {
		seen[name] = struct{}{}
	}
	for _, name := range additional {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func newTaskCanonicalStateFileNames() []string {
	names := []string{
		"task.id",
		"worker.id",
		"worker.ready",
		"reviewer.id",
		"reviewer.ready",
		parentEvidenceLedgerPath,
		parentEvidenceLeasePath,
		taskDispositionStateFile,
	}
	return append(names, newTaskTransitionStateFileNames()...)
}
