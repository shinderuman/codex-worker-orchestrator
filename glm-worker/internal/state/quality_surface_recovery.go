package state

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func (s *StateStore) matchRecoveryTaskID(expectedTaskID string) error {
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if taskID != expectedTaskID {
		return fmt.Errorf("quality-surface recovery requires the caller-bound task %s, got %s", expectedTaskID, taskID)
	}
	return nil
}

func (s *StateStore) RecoverQualitySurfaceDecisionWait(expectedTaskID string) error {
	if err := s.matchRecoveryTaskID(expectedTaskID); err != nil {
		return err
	}
	if s.TaskStatus() != TaskStatusWaitingSolReview {
		return fmt.Errorf("quality-surface decision wait recovery requires waiting-sol-review, got %s", s.TaskStatus())
	}
	if !s.Exists("pending-decision") {
		return fmt.Errorf("quality-surface decision wait recovery requires the leftover pending decision marker")
	}
	checkpoint, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if err := retainedDecisionWaitApprovalCheckpoint(checkpoint); err != nil {
		return err
	}
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return fmt.Errorf("quality-surface decision wait recovery cannot read parent review state: %w", err)
	}
	switch label {
	case roundCommentNone, string(packet.StatusNeedsSolReview):
	default:
		return fmt.Errorf("quality-surface decision wait recovery requires no open parent review or an open %s review, got %s", packet.StatusNeedsSolReview, label)
	}
	return s.Remove("pending-decision")
}

func retainedDecisionWaitApprovalCheckpoint(checkpoint ResumeCheckpoint) error {
	if checkpoint.StopKind != ResumeStopNone || !checkpoint.QualitySurfaceApprovalPending {
		return fmt.Errorf("quality-surface decision wait recovery requires a retained unstopped approval checkpoint")
	}
	if checkpoint.CompletedResult == nil || checkpoint.CompletedResult.Status != packet.StatusImplemented {
		return fmt.Errorf("quality-surface decision wait recovery requires a completed IMPLEMENTED worker result")
	}
	if checkpoint.Stage != ResumeStageWorker || WorkerPhaseCategory(checkpoint.Phase) != WorkerPhaseCategoryDecision {
		return fmt.Errorf("quality-surface decision wait recovery requires the worker-decision checkpoint phase, got %s", checkpoint.Phase)
	}
	return nil
}

func (s *StateStore) RecoverApprovedQualitySurfaceReview(expectedTaskID string) error {
	if err := s.matchRecoveryTaskID(expectedTaskID); err != nil {
		return err
	}
	status := s.TaskStatus()
	if !StoppedTaskStatus(status) {
		return fmt.Errorf("approved quality-surface review recovery requires a stopped task, got %s", status)
	}
	checkpoint, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if err := retainedAutoFixStopProvenance(status, checkpoint); err != nil {
		return err
	}
	if s.Exists("pending-decision") {
		return fmt.Errorf("approved quality-surface review recovery requires no leftover pending decision marker")
	}
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return fmt.Errorf("approved quality-surface review recovery cannot read parent review state: %w", err)
	}
	if label != string(packet.StatusNeedsSolReview) {
		return fmt.Errorf("approved quality-surface review recovery requires a stale %s parent review, got %s", packet.StatusNeedsSolReview, label)
	}
	resolved, err := s.RecordParentOutcome(ParentOutcomeAccepted, "", "")
	if err != nil {
		return fmt.Errorf("approved quality-surface review recovery cannot close the stale parent review: %w", err)
	}
	after, err := s.CurrentParentReviewLabel()
	if err != nil {
		return fmt.Errorf("approved quality-surface review recovery cannot verify parent review closure: %w", err)
	}
	if !resolved || after != roundCommentNone {
		return fmt.Errorf("approved quality-surface review recovery could not close the stale %s parent review", packet.StatusNeedsSolReview)
	}
	return nil
}

func retainedAutoFixStopProvenance(status TaskStatus, checkpoint ResumeCheckpoint) error {
	if !checkpoint.IsStopped() || checkpoint.StopKind.TaskStatus() != status {
		return fmt.Errorf("approved quality-surface review recovery requires a stop checkpoint matching %s", status)
	}
	if checkpoint.Stage != ResumeStageAutoFix || WorkerPhaseCategory(checkpoint.Phase) != WorkerPhaseCategoryAutoFix {
		return fmt.Errorf("approved quality-surface review recovery requires the post-approval auto-fix checkpoint provenance, got stage=%s phase=%s", checkpoint.Stage, checkpoint.Phase)
	}
	return nil
}

func StoppedTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusRateLimited,
		TaskStatusProviderUnavailable,
		TaskStatusInterrupted,
		TaskStatusGuardRecoverable,
		TaskStatusQualityGateRecoverable:
		return true
	default:
		return false
	}
}
