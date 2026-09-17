package state

import (
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) recoverOrphanedResetTaskContext(observedStatus TaskStatus) (string, TaskStatus, error) {
	taskID, status, found, err := s.currentStatsResetTaskContext(observedStatus)
	if err != nil || found {
		return taskID, status, err
	}
	return s.archivedResetTaskContext(observedStatus)
}

func (s *StateStore) currentStatsResetTaskContext(observedStatus TaskStatus) (string, TaskStatus, bool, error) {
	stats, err := s.CurrentTaskStats()
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, errUnsupportedTaskStatsVersion) {
		return "", observedStatus, false, nil
	}
	if err != nil {
		return "", observedStatus, false, fmt.Errorf("cannot verify orphaned reset task stats: %w", err)
	}
	if !ValidGeneratedUUID(stats.TaskID) || !stats.Status.Known() || stats.Status == TaskStatusNone {
		return "", observedStatus, false, fmt.Errorf("current task stats cannot prove orphaned reset task provenance")
	}
	if err := validateObservedResetStatus(observedStatus, stats.Status, "current task stats"); err != nil {
		return "", observedStatus, false, err
	}
	if err := s.validateResetParentTaskID(stats.TaskID); err != nil {
		return "", observedStatus, false, err
	}
	return stats.TaskID, stats.Status, true, nil
}

func (s *StateStore) archivedResetTaskContext(observedStatus TaskStatus) (string, TaskStatus, error) {
	parentState, err := s.rawParentReviewStateForReset()
	if errors.Is(err, os.ErrNotExist) {
		return "", observedStatus, nil
	}
	if err != nil {
		return "", observedStatus, err
	}
	evidence, err := s.ArchivedTaskStatsEvidence(parentState.TaskID)
	if err == nil && evidence.Proven {
		if err := validateObservedResetStatus(observedStatus, evidence.Status, "archived task stats"); err != nil {
			return "", observedStatus, err
		}
		return parentState.TaskID, evidence.Status, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", observedStatus, fmt.Errorf("cannot verify orphaned reset task archive: %w", err)
	}
	if observedStatus.Known() && observedStatus != TaskStatusNone {
		return parentState.TaskID, observedStatus, nil
	}
	return "", observedStatus, fmt.Errorf("orphaned task %s has no status provenance", parentState.TaskID)
}

func (s *StateStore) validateResetParentTaskID(taskID string) error {
	parentState, err := s.rawParentReviewStateForReset()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if parentState.TaskID != taskID {
		return fmt.Errorf("current task stats task %s does not match parent review task %s", taskID, parentState.TaskID)
	}
	return nil
}

func validateObservedResetStatus(observed, proven TaskStatus, source string) error {
	if observed != TaskStatusNone && observed != proven {
		return fmt.Errorf("task status %s does not match %s status %s", observed, source, proven)
	}
	return nil
}
