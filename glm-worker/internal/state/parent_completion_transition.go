package state

type parentCompletionTransitionResult struct {
	transitionErr      error
	rollbackStatusErr  error
	rollbackPendingErr error
}

type SessionRotationEvaluator func(acceptedRisk string) (*SessionRotationEvaluation, error)

type sessionRotationBuild func() (*SessionRotationEvaluation, error)

func (s *StateStore) commitParentCompletion(status TaskStatus, clearPending bool, build sessionRotationBuild) parentCompletionTransitionResult {
	previousStatus := s.TaskStatus()
	if clearPending {
		if err := s.Remove("pending-decision"); err != nil {
			return parentCompletionTransitionResult{transitionErr: err}
		}
	}
	if err := s.SetTaskStatus(status); err != nil {
		result := parentCompletionTransitionResult{transitionErr: err}
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
			return s.rollbackParentCompletion(previousStatus, clearPending, err)
		}
	}
	return parentCompletionTransitionResult{}
}

func (s *StateStore) rollbackParentCompletion(previousStatus TaskStatus, clearPending bool, transitionErr error) parentCompletionTransitionResult {
	result := parentCompletionTransitionResult{transitionErr: transitionErr}
	if err := s.SetTaskStatus(previousStatus); err != nil {
		result.rollbackStatusErr = err
	}
	if clearPending {
		result.rollbackPendingErr = s.Touch("pending-decision")
	}
	return result
}
