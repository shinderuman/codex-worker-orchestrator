package state

import (
	"errors"
	"fmt"
)

func (s *StateStore) AcceptParentReview() (bool, error) {
	if err := s.RequireParentReviewAcceptanceEvidence(); err != nil {
		return false, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return false, err
	}
	resolved, ok, resolveErr := s.resolveParentCompletionState(ParentOutcomeAccepted, SessionRotationTerminalAccept)
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}

	result := s.commitParentCompletion(TaskStatusAwaitingParentCompletion, false, nil)
	if result.transitionErr != nil {
		restoreErr := s.restoreLifecycleFile(review)
		if result.rollbackStatusErr != nil || restoreErr != nil {
			return false, fmt.Errorf("parent accept outcomeを保存できずrollbackにも失敗しました: outcome=%w rollback=%w", result.transitionErr, errors.Join(result.rollbackStatusErr, restoreErr))
		}
		return false, fmt.Errorf("parent accept outcomeを保存できません: %w", result.transitionErr)
	}

	s.projectParentCompletionOutcome(ParentOutcomeAccepted, resolved, SessionRotationTerminalAccept)
	s.appendParentOutcomeEvent(taskID, ParentPhaseAccept, ParentOutcomeAccepted, "", "", resolved)
	return true, nil
}

func sessionRotationBuildFor(evaluate SessionRotationEvaluator, acceptedRisk string) sessionRotationBuild {
	if evaluate == nil {
		return nil
	}
	return func() (*SessionRotationEvaluation, error) {
		return evaluate(acceptedRisk)
	}
}
