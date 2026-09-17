package state

import (
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) ValidateResetRequest(requested string) error {
	existing, err := s.CurrentTaskDisposition()
	if err == nil {
		if requested != "" && TaskDisposition(requested) != existing.Disposition {
			return fmt.Errorf("task reset disposition is already %s; cannot replace it with %s", existing.Disposition, requested)
		}
		if taskID := s.ReadOr("task.id", ""); taskID != "" && taskID != existing.TaskID {
			return fmt.Errorf("task reset disposition belongs to %s but current task is %s", existing.TaskID, taskID)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	taskID := s.ReadOr("task.id", "")
	status := s.TaskStatus()
	if taskID == "" && status == TaskStatusNone {
		if requested != "" {
			return fmt.Errorf("cannot record reset disposition %s without a current task", requested)
		}
		return nil
	}
	if status == TaskStatusComplete {
		if requested != "" {
			return fmt.Errorf("completed task cleanup does not accept a reset disposition")
		}
		return nil
	}
	if taskID == "" {
		return fmt.Errorf("cannot dispose task state %s without task.id provenance", status)
	}
	_, err = resolveResetDisposition(status, requested)
	return err
}
