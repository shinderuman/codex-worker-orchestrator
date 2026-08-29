package state

import "fmt"

// AcceptParentReview resolves the currently open parent review and transitions
// the task lifecycle to complete as one recoverable operation. The lifecycle
// write happens first so an outcome write failure can be retried against the
// still-open review after rollback.
func (s *StateStore) AcceptParentReview() (bool, error) {
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, nil
		}
	}
	resolved, ok, resolveErr := stats.resolveParentOutcome(ParentOutcomeAccepted, "")
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}

	previousStatus := s.TaskStatus()
	stats.Status = TaskStatusComplete
	if err := s.SetTaskStatus(TaskStatusComplete); err != nil {
		return false, err
	}
	if err := s.writeTaskStats(stats); err != nil {
		if rollbackErr := s.SetTaskStatus(previousStatus); rollbackErr != nil {
			return false, fmt.Errorf("parent accept outcomeを保存できずtask status rollbackにも失敗しました: outcome=%v rollback=%v", err, rollbackErr)
		}
		return false, fmt.Errorf("parent accept outcomeを保存できません: %w", err)
	}

	s.appendParentOutcomeEvent(stats.TaskID, ParentPhaseAccept, ParentOutcomeAccepted, "", resolved)
	return true, nil
}
