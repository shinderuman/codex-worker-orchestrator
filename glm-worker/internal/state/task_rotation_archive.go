package state

import (
	"errors"
	"fmt"
	"os"
)

type taskStatsArchiveSnapshot struct {
	data   []byte
	exists bool
}

func (s *StateStore) archiveCurrentStatsForTaskRotation(parentIdentity *ParentCodexIdentity, stats TaskStats) error {
	archivePath := s.TaskStatsArchivePath(stats.TaskID)
	snapshot, err := captureTaskStatsArchiveSnapshot(archivePath)
	if err != nil {
		return err
	}

	resolved, hadOpen, _ := stats.resolveParentOutcome(ParentOutcomeUnknown, "", "")
	if err := s.archiveCurrentStats(parentIdentity, stats); err != nil {
		if rollbackErr := restoreTaskStatsArchiveSnapshot(archivePath, snapshot); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("task stats archiveをrollbackできません: %w", rollbackErr))
		}
		return err
	}
	if hadOpen {
		s.appendParentOutcomeEvent(stats.TaskID, ParentPhaseClose, ParentOutcomeUnknown, "", "", resolved)
	}
	return nil
}

func captureTaskStatsArchiveSnapshot(path string) (taskStatsArchiveSnapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return taskStatsArchiveSnapshot{}, nil
	}
	if err != nil {
		return taskStatsArchiveSnapshot{}, fmt.Errorf("既存task stats archiveを読めません: %w", err)
	}
	return taskStatsArchiveSnapshot{data: data, exists: true}, nil
}

func restoreTaskStatsArchiveSnapshot(path string, snapshot taskStatsArchiveSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeFileAtomic(path, snapshot.data, 0o600)
}
