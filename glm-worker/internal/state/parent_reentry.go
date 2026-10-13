package state

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ControllerReentryAuthority struct {
	PredecessorAttemptID string
	AttemptID            string
	TaskPath             string
	TaskContractDigest   string
}

func (s *StateStore) ReenterAcceptedCompletionFromController(authority ControllerReentryAuthority) (bool, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return false, nil
	}
	completion, err := s.controllerReentryCompletionAuthority(authority)
	if err != nil {
		return false, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return false, err
	}
	risk := reentryReviewRiskFloor(completion.Risk)
	if err := s.applyControllerReentryTransition(risk); err != nil {
		return false, err
	}
	s.recordReopenedParentOutcome(taskID, ParentOriginUserAmendment, ParentCauseSolGate, risk)
	return true, nil
}

func (s *StateStore) controllerReentryCompletionAuthority(authority ControllerReentryAuthority) (*ParentCompletionOutcome, error) {
	completion, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return nil, err
	}
	if completion == nil || completion.Terminal != SessionRotationTerminalAccept {
		return nil, fmt.Errorf("controller runtime reentry requires an accepted parent completion outcome")
	}
	binding, err := s.LoadControllerRuntimeBinding()
	if err != nil {
		return nil, err
	}
	if err := verifyControllerReentryBinding(binding, authority); err != nil {
		return nil, err
	}
	return completion, nil
}

func verifyControllerReentryBinding(binding ControllerRuntimeBinding, authority ControllerReentryAuthority) error {
	if authority.AttemptID == "" || binding.AttemptID == authority.AttemptID || binding.AttemptID != authority.PredecessorAttemptID {
		return fmt.Errorf("workflow session does not belong to the canonical attempt predecessor")
	}
	if binding.TaskPath != authority.TaskPath || binding.TaskContractDigest != authority.TaskContractDigest {
		return fmt.Errorf("controller runtime reentry task identity does not match the workflow binding")
	}
	return nil
}

func (s *StateStore) applyControllerReentryTransition(risk string) error {
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	status, err := s.snapshotLifecycleFile("task.status")
	if err != nil {
		return err
	}
	if err := s.openParentReviewState(string(packet.StatusNeedsSolReview), risk, ParentReviewProducer{}, false); err != nil {
		return s.rollbackLifecycleFiles(err, review, status)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, review, status)
	}
	return nil
}

func reentryReviewRiskFloor(completionRisk string) string {
	if completionRisk != string(packet.RiskLow) {
		return completionRisk
	}
	return string(packet.RiskHigh)
}
