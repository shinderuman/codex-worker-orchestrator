package state

import (
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) recoverOrphanedResetTaskContext(observedStatus TaskStatus) (string, TaskStatus, error) {
	parentState, err := s.rawParentReviewStateForReset()
	if errors.Is(err, os.ErrNotExist) {
		if observedStatus == TaskStatusNone {
			return "", observedStatus, nil
		}
		return "", observedStatus, fmt.Errorf("cannot dispose task state %s without task.id provenance", observedStatus)
	}
	if err != nil {
		return "", observedStatus, err
	}
	return "", observedStatus, fmt.Errorf(
		"orphaned task %s has no durable reset disposition; cannot recover task identity or status after task.id loss",
		parentState.TaskID,
	)
}
