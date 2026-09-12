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

func (s *StateStore) commitNewTaskCanonicalState(taskID string) error {
	lock, err := repolock.AcquireWait(s.Path(ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()

	snapshot, err := s.captureNewTaskTransitionSnapshot()
	if err != nil {
		return err
	}
	if err := s.applyNewTaskCanonicalState(taskID); err != nil {
		if rollbackErr := s.restoreNewTaskTransitionSnapshot(snapshot); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("new task transitionをrollbackできません: %w", rollbackErr))
		}
		return err
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
	snapshot := make(newTaskTransitionSnapshot)
	for _, name := range newTaskCanonicalStateFileNames() {
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
	var result error
	for _, name := range newTaskCanonicalStateFileNames() {
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

func newTaskCanonicalStateFileNames() []string {
	names := []string{
		"task.id",
		"worker.id",
		"worker.ready",
		"reviewer.id",
		"reviewer.ready",
		parentEvidenceLedgerPath,
		parentEvidenceLeasePath,
	}
	return append(names, newTaskTransitionStateFileNames()...)
}
