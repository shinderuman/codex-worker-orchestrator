package state

import (
	"errors"
	"fmt"
	"os"
)

type taskRotationFileSnapshot struct {
	data   []byte
	exists bool
}

func (s *StateStore) archiveCurrentStatsForTaskRotation(parentIdentity *ParentCodexIdentity, stats TaskStats) error {
	archivePath := s.TaskStatsArchivePath(stats.TaskID)
	archiveSnapshot, err := captureTaskRotationFileSnapshot(archivePath)
	if err != nil {
		return err
	}
	telemetryPath := s.ModelCallLogPath(stats.TaskID)
	telemetrySnapshot, err := captureTaskRotationFileSnapshot(telemetryPath)
	if err != nil {
		return err
	}

	if err := s.archiveCurrentStats(parentIdentity, stats); err != nil {
		rollbackErr := errors.Join(
			restoreTaskRotationFileSnapshot(archivePath, archiveSnapshot),
			restoreTaskRotationFileSnapshot(telemetryPath, telemetrySnapshot),
		)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("task rotation archiveをrollbackできません: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func captureTaskRotationFileSnapshot(path string) (taskRotationFileSnapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return taskRotationFileSnapshot{}, nil
	}
	if err != nil {
		return taskRotationFileSnapshot{}, fmt.Errorf("task rotation rollback対象を読めません: %w", err)
	}
	return taskRotationFileSnapshot{data: data, exists: true}, nil
}

func restoreTaskRotationFileSnapshot(path string, snapshot taskRotationFileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeFileAtomic(path, snapshot.data, 0o600)
}
