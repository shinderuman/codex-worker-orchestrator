package state

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const ParentOutcomeNoGo = "no-go"

// ObservationNoGoEligible reports whether the current parent decision boundary
// belongs to a read-only PoC/observation task that may be terminally withdrawn.
func (s *StateStore) ObservationNoGoEligible() bool {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return false
	}
	stats, err := s.loadTaskStats()
	if err != nil || stats.ParentReviewOpen == nil || stats.ParentReviewOpen.PacketStatus != string(packet.StatusNeedsSolDecision) {
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

// CompleteObservationNoGo resolves a pending PoC/observation Go/No-Go boundary
// without another model dispatch. The caller must hold the repository lock.
func (s *StateStore) CompleteObservationNoGo() (bool, error) {
	if !s.ObservationNoGoEligible() {
		return false, fmt.Errorf("terminal no-go is only available for a pending PoC/observation Sol decision")
	}

	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return false, fmt.Errorf("terminal no-go task statsを復旧できません: %w", err)
		}
	}
	if stats.ParentReviewOpen == nil {
		return false, fmt.Errorf("terminal no-go has no open parent decision")
	}
	resolved := *stats.ParentReviewOpen
	stats.ParentReviewOpen = nil
	stats.Status = TaskStatusComplete
	addInt(&stats.ParentOutcomes, ParentOutcomeNoGo, 1)
	model := resolved.ModelAlias
	if model == "" {
		model = ParentOriginUnknown
	}
	addInt(&stats.ParentOutcomesByModel, model, 1)
	risk := resolved.Risk
	if risk == "" {
		risk = ParentOriginUnknown
	}
	addInt(&stats.ParentOutcomesByRisk, risk, 1)

	previousStatus := s.TaskStatus()
	if err := s.Remove("pending-decision"); err != nil {
		return false, err
	}
	if err := s.SetTaskStatus(TaskStatusComplete); err != nil {
		_ = s.Touch("pending-decision")
		return false, err
	}
	if err := s.writeTaskStats(stats); err != nil {
		rollbackStatusErr := s.SetTaskStatus(previousStatus)
		rollbackPendingErr := s.Touch("pending-decision")
		if rollbackStatusErr != nil || rollbackPendingErr != nil {
			return false, fmt.Errorf("terminal no-go outcomeを保存できずstate rollbackにも失敗しました: outcome=%w status=%v pending=%v", err, rollbackStatusErr, rollbackPendingErr)
		}
		return false, fmt.Errorf("terminal no-go outcomeを保存できません: %w", err)
	}

	s.appendParentOutcomeEvent(stats.TaskID, ParentPhaseClose, ParentOutcomeNoGo, "", resolved)
	return true, nil
}
