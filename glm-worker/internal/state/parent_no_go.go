package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const ParentOutcomeNoGo = "no-go"

func (s *StateStore) ObservationNoGoEligible() bool {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return false
	}
	open, err := s.CurrentParentReview()
	if err != nil || open == nil || open.PacketStatus != string(packet.StatusNeedsSolDecision) {
		return false
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false
	}
	content, err := os.ReadFile(s.TaskAuthorityContentPath(taskID))
	if err != nil {
		return false
	}
	declaration, err := taskcontract.ParseExternalFeasibility(content)
	if err != nil {
		return false
	}
	return declaration.Status == taskcontract.StatusPoC || declaration.Status == taskcontract.StatusObservation
}

func (s *StateStore) AwaitObservationNoGo() (bool, error) {
	if !s.ObservationNoGoEligible() {
		return false, fmt.Errorf("terminal no-go is only available for a pending PoC/observation Sol decision")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
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
