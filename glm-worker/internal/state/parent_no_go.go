package state

import (
	"errors"
	"fmt"
)

const ParentOutcomeNoGo = "no-go"

func (s *StateStore) AwaitObservationNoGo(expected ObservationExecutionAdmission) (bool, error) {
	if err := s.ValidateObservationExecuteAdmission(expected); err != nil {
		return false, fmt.Errorf("terminal no-go lifecycle admission is no longer valid: %w", err)
	}
	taskID := expected.TaskID
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return false, err
	}
	resolved, ok, err := s.resolveParentCompletionState(ParentOutcomeNoGo, SessionRotationTerminalNoGo)
	if err != nil || !ok {
		return ok, err
	}

	result := s.commitParentCompletion(TaskStatusAwaitingParentCompletion, true, nil)
	if result.transitionErr != nil {
		restoreErr := s.restoreLifecycleFile(review)
		if result.rollbackStatusErr != nil || result.rollbackPendingErr != nil || restoreErr != nil {
			return false, fmt.Errorf("terminal no-go outcomeを保存できずstate rollbackにも失敗しました: %w", errors.Join(result.transitionErr, result.rollbackStatusErr, result.rollbackPendingErr, restoreErr))
		}
		return false, fmt.Errorf("terminal no-go outcomeを保存できません: %w", result.transitionErr)
	}

	s.projectParentCompletionOutcome(ParentOutcomeNoGo, resolved, SessionRotationTerminalNoGo)
	s.appendParentOutcomeEvent(taskID, ParentPhaseClose, ParentOutcomeNoGo, "", "", resolved)
	return true, nil
}
