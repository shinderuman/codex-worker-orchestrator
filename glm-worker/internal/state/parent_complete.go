package state

import (
	"errors"
	"fmt"
)

func (s *StateStore) CompleteParentAwaiting(evaluate SessionRotationEvaluator) (bool, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return false, fmt.Errorf("parent completion transition requires awaiting-parent-completion, got %s", s.TaskStatus())
	}
	acceptedRisk := ""
	if evaluate != nil {
		_, risk, found := s.CompletionOutcomeEvidence()
		if !found {
			return false, fmt.Errorf("parent completion outcome evidence is unavailable")
		}
		acceptedRisk = risk
	}
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, fmt.Errorf("awaiting task statsを読み込めません: %w", err)
		}
	}
	stats.Status = TaskStatusComplete
	result := s.commitParentCompletion(stats, TaskStatusComplete, false, sessionRotationBuildFor(evaluate, acceptedRisk))
	if result.transitionErr != nil {
		if result.rollbackStatusErr != nil || result.rollbackPendingErr != nil {
			return false, fmt.Errorf("parent completion outcomeを保存できずstate rollbackにも失敗しました: outcome=%w rollback=%w", result.transitionErr, errors.Join(result.rollbackStatusErr, result.rollbackPendingErr))
		}
		return false, fmt.Errorf("parent completion outcomeを保存できません: %w", result.transitionErr)
	}
	return true, nil
}

func (s *StateStore) CompletionOutcomeEvidence() (string, string, bool) {
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return "", "", false
	}
	logs, err := s.ReadModelCallLogs(taskID)
	if err != nil {
		return "", "", false
	}
	for index := len(logs) - 1; index >= 0; index-- {
		record := logs[index]
		if record.CallType != CallTypeEvent {
			continue
		}
		switch record.Phase {
		case ParentPhaseAccept:
			if record.Outcome == ParentOutcomeAccepted {
				return SessionRotationTerminalAccept, record.WorkerReportedRisk, true
			}
		case ParentPhaseClose:
			if record.Outcome == ParentOutcomeNoGo {
				return SessionRotationTerminalNoGo, record.WorkerReportedRisk, true
			}
		}
	}
	return "", "", false
}
