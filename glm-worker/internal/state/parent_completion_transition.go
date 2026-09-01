package state

type parentCompletionTransitionResult struct {
	transitionErr      error
	rollbackStatusErr  error
	rollbackPendingErr error
}

func (s *StateStore) commitParentCompletion(stats TaskStats, clearPending bool) parentCompletionTransitionResult {
	previousStatus := s.TaskStatus()
	if clearPending {
		if err := s.Remove("pending-decision"); err != nil {
			return parentCompletionTransitionResult{transitionErr: err}
		}
	}
	if err := s.SetTaskStatus(TaskStatusComplete); err != nil {
		result := parentCompletionTransitionResult{transitionErr: err}
		if clearPending {
			result.rollbackPendingErr = s.Touch("pending-decision")
		}
		return result
	}
	if err := s.writeTaskStats(stats); err != nil {
		result := parentCompletionTransitionResult{
			transitionErr:     err,
			rollbackStatusErr: s.SetTaskStatus(previousStatus),
		}
		if clearPending {
			result.rollbackPendingErr = s.Touch("pending-decision")
		}
		return result
	}
	return parentCompletionTransitionResult{}
}
