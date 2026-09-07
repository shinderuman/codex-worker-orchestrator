package state

type parentCompletionTransitionResult struct {
	transitionErr      error
	rollbackStatusErr  error
	rollbackPendingErr error
}

type SessionRotationEvaluator func(acceptedRisk string) (*SessionRotationEvaluation, error)

type sessionRotationBuild func() (*SessionRotationEvaluation, error)

func (s *StateStore) commitParentCompletion(stats TaskStats, clearPending bool, build sessionRotationBuild) parentCompletionTransitionResult {
	previousStatus := s.TaskStatus()
	originalStats, originalStatsErr := s.loadTaskStats()
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
	if build != nil {
		evaluation, err := build()
		if err == nil {
			err = s.commitSessionRotation(evaluation)
		}
		if err != nil {
			return s.rollbackParentCompletionWithRotation(previousStatus, originalStats, originalStatsErr, clearPending, err)
		}
	}
	return parentCompletionTransitionResult{}
}

func (s *StateStore) rollbackParentCompletionWithRotation(
	previousStatus TaskStatus,
	originalStats TaskStats,
	originalStatsErr error,
	clearPending bool,
	transitionErr error,
) parentCompletionTransitionResult {
	result := parentCompletionTransitionResult{transitionErr: transitionErr}
	if originalStatsErr == nil {
		result.rollbackStatusErr = s.writeTaskStats(originalStats)
	}
	if err := s.SetTaskStatus(previousStatus); err != nil {
		result.rollbackStatusErr = err
	}
	if clearPending {
		result.rollbackPendingErr = s.Touch("pending-decision")
	}
	return result
}
