package state

import (
	"errors"
	"fmt"
)

func (s *StateStore) CompleteParentAwaiting(evaluate SessionRotationEvaluator) (bool, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return false, fmt.Errorf("parent completion transition requires awaiting-parent-completion, got %s", s.TaskStatus())
	}
	outcome, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return false, fmt.Errorf("parent completion outcome is unreadable: %w", err)
	}
	if outcome == nil {
		return false, fmt.Errorf("parent completion outcome is unavailable")
	}
	result := s.commitParentCompletion(TaskStatusComplete, false, sessionRotationBuildFor(evaluate, outcome.Risk))
	if result.transitionErr != nil {
		if result.rollbackStatusErr != nil || result.rollbackPendingErr != nil {
			return false, fmt.Errorf("parent completion outcomeを保存できずstate rollbackにも失敗しました: outcome=%w rollback=%w", result.transitionErr, errors.Join(result.rollbackStatusErr, result.rollbackPendingErr))
		}
		return false, fmt.Errorf("parent completion outcomeを保存できません: %w", result.transitionErr)
	}
	s.projectCurrentParentCompletionOutcome(*outcome)
	return true, nil
}
