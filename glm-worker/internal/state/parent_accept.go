package state

import "fmt"

func (s *StateStore) AcceptParentReview() (bool, error) {
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, nil
		}
	}
	resolved, ok, resolveErr := stats.resolveParentOutcome(ParentOutcomeAccepted, "", "")
	if !ok || resolveErr != nil {
		return ok, resolveErr
	}

	stats.Status = TaskStatusAwaitingParentCompletion
	stats.AcceptedRisk = resolved.Risk
	stats.CompletionTerminal = SessionRotationTerminalAccept
	result := s.commitParentCompletion(stats, TaskStatusAwaitingParentCompletion, false, nil)
	if result.transitionErr != nil {
		if result.rollbackStatusErr != nil {
			return false, fmt.Errorf("parent accept outcomeを保存できずtask status rollbackにも失敗しました: outcome=%w rollback=%w", result.transitionErr, result.rollbackStatusErr)
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
