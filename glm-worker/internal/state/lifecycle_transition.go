package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type lifecycleFileSnapshot struct {
	name   string
	data   []byte
	exists bool
}

type ParentActionRollback struct {
	status  TaskStatus
	pending lifecycleFileSnapshot
	review  lifecycleFileSnapshot
}

func (s *StateStore) BeginParentDecision() (ParentActionRollback, error) {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return ParentActionRollback{}, fmt.Errorf("parent decision transition requires waiting-decision with pending decision")
	}
	rollback, err := s.snapshotParentActionRollback()
	if err != nil {
		return ParentActionRollback{}, err
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return ParentActionRollback{}, s.rollbackParentAction(rollback, err)
	}
	s.RecordDecision()
	if _, err := s.RecordParentOutcome(ParentOutcomeDecision, "", ""); err != nil {
		return ParentActionRollback{}, s.rollbackParentAction(rollback, err)
	}
	return rollback, nil
}

func (s *StateStore) BeginParentFix(origin, cause string) (ParentActionRollback, error) {
	if s.TaskStatus() != TaskStatusWaitingSolReview || s.Exists("pending-decision") {
		return ParentActionRollback{}, fmt.Errorf("parent fix transition requires waiting-sol-review without pending decision")
	}
	rollback, err := s.snapshotParentActionRollback()
	if err != nil {
		return ParentActionRollback{}, err
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return ParentActionRollback{}, s.rollbackParentAction(rollback, err)
	}
	s.RecordFix()
	if _, err := s.RecordParentOutcome(ParentOutcomeFix, origin, cause); err != nil {
		return ParentActionRollback{}, s.rollbackParentAction(rollback, err)
	}
	return rollback, nil
}

func (s *StateStore) RollbackParentAction(rollback ParentActionRollback, cause error) error {
	return s.rollbackParentAction(rollback, cause)
}

func (s *StateStore) RecoverParentActionBegin(target TaskStatus) error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("parent action recovery requires the leftover active task, got %s", s.TaskStatus())
	}
	if _, err := s.LoadResumeCheckpoint(); err == nil {
		return fmt.Errorf("parent action recovery requires no resume checkpoint")
	} else if !errors.Is(err, ErrNoResumeCheckpoint) {
		return err
	}
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return fmt.Errorf("parent action recovery cannot read parent review state: %w", err)
	}
	if label != roundCommentNone {
		return fmt.Errorf("parent action recovery requires no open parent review, got %s", label)
	}
	pending := s.Exists("pending-decision")
	switch target {
	case TaskStatusWaitingDecision:
		if !pending {
			return fmt.Errorf("parent action recovery for a decision requires the pending decision payload")
		}
	case TaskStatusWaitingSolReview:
		if pending {
			return fmt.Errorf("parent action recovery for a fix requires no pending decision")
		}
	default:
		return fmt.Errorf("parent action recovery target %s is not a parent waiting state", target)
	}
	return s.SetTaskStatus(target)
}

func (s *StateStore) snapshotParentActionRollback() (ParentActionRollback, error) {
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return ParentActionRollback{}, err
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return ParentActionRollback{}, err
	}
	return ParentActionRollback{status: s.TaskStatus(), pending: pending, review: review}, nil
}

func (s *StateStore) rollbackParentAction(rollback ParentActionRollback, cause error) error {
	if err := s.restoreLifecycleFile(rollback.review); err != nil {
		return joinParentActionRollbackFailure(cause, err)
	}
	if err := s.restoreLifecycleFile(rollback.pending); err != nil {
		return joinParentActionRollbackFailure(cause, err)
	}
	if err := s.SetTaskStatus(rollback.status); err != nil {
		return joinParentActionRollbackFailure(cause, err)
	}
	return cause
}

func joinParentActionRollbackFailure(cause error, rollbackErr error) error {
	return fmt.Errorf("parent action begin failed and rollback failed: begin=%w rollback=%w", cause, rollbackErr)
}

func (s *StateStore) WaitForDecision() error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("wait-for-decision transition requires active task, got %s", s.TaskStatus())
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	lease, err := s.snapshotLifecycleFile(parentEvidenceLeasePath)
	if err != nil {
		return err
	}
	if err := s.AdvanceParentEvidenceLease(); err != nil {
		return err
	}
	if err := s.Touch("pending-decision"); err != nil {
		return s.rollbackLifecycleFiles(err, pending, lease)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		return s.rollbackLifecycleFiles(err, pending, lease)
	}
	return nil
}

func (s *StateStore) ContinueAfterWorkerResult() error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("continue-worker transition requires active task, got %s", s.TaskStatus())
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if err := s.Remove("pending-decision"); err != nil {
		return err
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return s.rollbackLifecycleFiles(err, pending)
	}
	return nil
}

func (s *StateStore) FinishReview(status TaskStatus) error {
	if status != TaskStatusComplete && status != TaskStatusWaitingSolReview {
		return fmt.Errorf("review transition cannot enter status %s", status)
	}
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("review transition requires active task, got %s", s.TaskStatus())
	}
	if status != TaskStatusWaitingSolReview {
		return s.SetTaskStatus(status)
	}
	lease, err := s.snapshotLifecycleFile(parentEvidenceLeasePath)
	if err != nil {
		return err
	}
	if err := s.AdvanceParentEvidenceLease(); err != nil {
		return err
	}
	if err := s.SetTaskStatus(status); err != nil {
		return s.rollbackLifecycleFiles(err, lease)
	}
	return nil
}

func (s *StateStore) WaitForSolReview() error {
	previous := s.TaskStatus()
	switch previous {
	case TaskStatusNone, TaskStatusActive, TaskStatusWaitingDecision, TaskStatusWaitingSolReview:
	default:
		return fmt.Errorf("wait-for-sol-review transition is invalid from %s", s.TaskStatus())
	}
	if previous == TaskStatusWaitingSolReview {
		return s.SetTaskStatus(TaskStatusWaitingSolReview)
	}
	lease, err := s.snapshotLifecycleFile(parentEvidenceLeasePath)
	if err != nil {
		return err
	}
	if err := s.AdvanceParentEvidenceLease(); err != nil {
		return err
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, lease)
	}
	return nil
}

func (s *StateStore) WaitForQualitySurfaceReview(phase string) error {
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if pending.exists {
		if err := s.verifyPendingDecisionContinuation(phase); err != nil {
			return err
		}
	}
	if err := s.Remove("pending-decision"); err != nil {
		return err
	}
	if err := s.WaitForSolReview(); err != nil {
		return s.rollbackLifecycleFiles(err, pending)
	}
	return nil
}

func (s *StateStore) verifyPendingDecisionContinuation(phase string) error {
	if WorkerPhaseCategory(phase) != WorkerPhaseCategoryDecision {
		return fmt.Errorf("quality-surface review wait cannot clear a pending decision outside the worker-decision continuation, got %s", phase)
	}
	if decision, err := s.Read("last-decision"); err != nil || decision == "" {
		return fmt.Errorf("quality-surface review wait requires the saved decision binding behind the pending decision marker")
	}
	return nil
}

func (s *StateStore) EnterQualitySurfaceApprovalWait(checkpoint ResumeCheckpoint) error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("quality-surface approval wait requires an active task, got %s", s.TaskStatus())
	}
	if !checkpoint.QualitySurfaceApprovalPending || checkpoint.IsStopped() {
		return fmt.Errorf("quality-surface approval wait requires an unstopped approval checkpoint")
	}
	if checkpoint.CompletedResult == nil {
		return fmt.Errorf("quality-surface approval wait requires a completed worker result")
	}
	resume, err := s.snapshotLifecycleFile(resumeStateFile)
	if err != nil {
		return err
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	lease, err := s.snapshotLifecycleFile(parentEvidenceLeasePath)
	if err != nil {
		return err
	}
	if err := s.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if err := s.Remove("pending-decision"); err != nil {
		return s.rollbackLifecycleFiles(err, resume)
	}
	if err := s.AdvanceParentEvidenceLease(); err != nil {
		return s.rollbackLifecycleFiles(err, resume, pending, lease)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, resume, pending, lease)
	}
	return nil
}

func (s *StateStore) DiscardResumeAndWaitForSolReview() error {
	resume, err := s.snapshotLifecycleFile(resumeStateFile)
	if err != nil {
		return err
	}
	if err := s.ClearResumeCheckpoint(); err != nil {
		return err
	}
	if err := s.WaitForSolReview(); err != nil {
		return s.rollbackLifecycleFiles(err, resume)
	}
	return nil
}

func (s *StateStore) ActivateQualitySurfaceApproval() error {
	if s.TaskStatus() != TaskStatusWaitingSolReview {
		return fmt.Errorf("quality-surface activation requires waiting-sol-review, got %s", s.TaskStatus())
	}
	checkpoint, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if !checkpoint.QualitySurfaceApprovalPending || checkpoint.IsStopped() {
		return fmt.Errorf("quality-surface activation requires retained approval checkpoint")
	}
	stats, err := s.snapshotLifecycleFile(currentStatsFile)
	if err != nil {
		return err
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	resume, err := s.snapshotLifecycleFile(resumeStateFile)
	if err != nil {
		return err
	}
	if err := s.closeApprovedQualitySurfaceReview(); err != nil {
		return err
	}
	if err := s.ClearResumeCheckpoint(); err != nil {
		return s.rollbackLifecycleFiles(err, stats, review, resume)
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return s.rollbackLifecycleFiles(err, stats, review, resume)
	}
	return nil
}

func (s *StateStore) closeApprovedQualitySurfaceReview() error {
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return fmt.Errorf("quality-surface activation cannot read parent review state: %w", err)
	}
	switch label {
	case roundCommentNone:
		return nil
	case string(packet.StatusNeedsSolReview):
		resolved, err := s.RecordParentOutcome(ParentOutcomeAccepted, "", "")
		if err != nil {
			return fmt.Errorf("quality-surface activation cannot close the open parent review: %w", err)
		}
		after, err := s.CurrentParentReviewLabel()
		if err != nil {
			return fmt.Errorf("quality-surface activation cannot verify parent review closure: %w", err)
		}
		if !resolved || after != roundCommentNone {
			return fmt.Errorf("quality-surface activation could not close the open %s review", packet.StatusNeedsSolReview)
		}
		return nil
	default:
		return fmt.Errorf("quality-surface activation requires no open parent review or an open %s review, got %s", packet.StatusNeedsSolReview, label)
	}
}

func (s *StateStore) EnterStop(checkpoint ResumeCheckpoint) error {
	if !checkpoint.IsStopped() {
		return fmt.Errorf("stop transition requires a resumable stop kind")
	}
	if checkpoint.StopKind.TaskStatus() == TaskStatusActive {
		return fmt.Errorf("stop transition has no stopped task status for %q", checkpoint.StopKind)
	}
	resume, err := s.snapshotLifecycleFile(resumeStateFile)
	if err != nil {
		if info, statErr := os.Stat(s.Path(resumeStateFile)); statErr == nil && info.IsDir() {
			return s.SaveResumeCheckpoint(checkpoint)
		}
		return err
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if err := s.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if checkpoint.StopKind == ResumeStopGuardRecoverable || checkpoint.StopKind == ResumeStopQualityGate {
		if err := s.Remove("pending-decision"); err != nil {
			return s.rollbackLifecycleFiles(err, resume)
		}
	}
	if err := s.SetTaskStatus(checkpoint.StopKind.TaskStatus()); err != nil {
		return s.rollbackLifecycleFiles(err, resume, pending)
	}
	return nil
}

func (s *StateStore) BeginResume(checkpoint ResumeCheckpoint) error {
	if !checkpoint.IsStopped() {
		return fmt.Errorf("resume transition requires stopped checkpoint")
	}
	expected := checkpoint.StopKind.TaskStatus()
	if s.TaskStatus() != expected {
		return fmt.Errorf("resume transition status mismatch: status=%s stop=%s", s.TaskStatus(), checkpoint.StopKind)
	}
	saved, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if saved.StopKind != checkpoint.StopKind {
		return fmt.Errorf("resume transition checkpoint mismatch: saved=%s requested=%s", saved.StopKind, checkpoint.StopKind)
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return err
	}
	s.RecordResume()
	return nil
}

func (s *StateStore) RestoreResumeStop(previous ResumeCheckpoint) error {
	if s.TaskStatus() != TaskStatusActive {
		return nil
	}
	checkpoint := previous
	if saved, err := s.LoadResumeCheckpoint(); err == nil && saved.IsStopped() {
		checkpoint = saved
	}
	if !checkpoint.IsStopped() {
		return fmt.Errorf("cannot restore resume stop without stopped checkpoint")
	}
	if err := s.EnterStop(checkpoint); err != nil {
		return fmt.Errorf("restore resume stop: %w", err)
	}
	return nil
}

func (s *StateStore) CompleteLifecycle() error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("complete transition requires active task, got %s", s.TaskStatus())
	}
	return s.SetTaskStatus(TaskStatusComplete)
}

func (s *StateStore) snapshotLifecycleFile(name string) (lifecycleFileSnapshot, error) {
	data, err := os.ReadFile(s.Path(name))
	if errors.Is(err, os.ErrNotExist) {
		return lifecycleFileSnapshot{name: name}, nil
	}
	if err != nil {
		return lifecycleFileSnapshot{}, fmt.Errorf("state %sをtransition前に読めません: %w", name, err)
	}
	return lifecycleFileSnapshot{name: name, data: data, exists: true}, nil
}

func (s *StateStore) rollbackLifecycleFiles(cause error, snapshots ...lifecycleFileSnapshot) error {
	var rollbackErr error
	for i := len(snapshots) - 1; i >= 0; i-- {
		if err := s.restoreLifecycleFile(snapshots[i]); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if rollbackErr != nil {
		return fmt.Errorf("lifecycle transition failed and rollback failed: transition=%w rollback=%w", cause, rollbackErr)
	}
	return cause
}

func (s *StateStore) restoreLifecycleFile(snapshot lifecycleFileSnapshot) error {
	if !snapshot.exists {
		return s.Remove(snapshot.name)
	}
	if err := writeFileAtomic(s.Path(snapshot.name), snapshot.data, 0o600); err != nil {
		return fmt.Errorf("state %sをrollbackできません: %w", snapshot.name, err)
	}
	return nil
}
