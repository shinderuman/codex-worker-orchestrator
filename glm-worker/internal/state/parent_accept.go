package state

import (
	"errors"
	"fmt"
)

func (s *StateStore) AcceptParentReview() (bool, error) {
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return false, err
	}
	resolved, ok, resolveErr := s.resolveParentReviewState(ParentOutcomeAccepted, "", "")
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}

	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			if restoreErr := s.restoreLifecycleFile(review); restoreErr != nil {
				return false, errors.Join(err, restoreErr)
			}
			return false, nil
		}
	}
	stats.ParentReviewOpen = nil
	stats.recordParentOutcome(ParentOutcomeAccepted, "", resolved)
	stats.Status = TaskStatusAwaitingParentCompletion
	stats.AcceptedRisk = resolved.Risk
	stats.CompletionTerminal = SessionRotationTerminalAccept
	result := s.commitParentCompletion(stats, TaskStatusAwaitingParentCompletion, false, nil)
	if result.transitionErr != nil {
		restoreErr := s.restoreLifecycleFile(review)
		if result.rollbackStatusErr != nil || restoreErr != nil {
			return false, fmt.Errorf("parent accept outcomeを保存できずrollbackにも失敗しました: outcome=%w rollback=%w", result.transitionErr, errors.Join(result.rollbackStatusErr, restoreErr))
		}
		return false, fmt.Errorf("parent accept outcomeを保存できません: %w", result.transitionErr)
	}

	s.appendParentOutcomeEvent(stats.TaskID, ParentPhaseAccept, ParentOutcomeAccepted, "", "", resolved)
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
