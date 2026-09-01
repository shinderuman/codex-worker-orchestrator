package state

import (
	"errors"
	"fmt"
	"os"
)

type lifecycleFileSnapshot struct {
	name   string
	data   []byte
	exists bool
}

func (s *StateStore) BeginParentDecision() error {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return fmt.Errorf("parent decision transition requires waiting-decision with pending decision")
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return err
	}
	s.RecordDecision()
	_, err := s.RecordParentOutcome(ParentOutcomeDecision, "")
	return err
}

func (s *StateStore) BeginParentFix(origin string) error {
	if s.TaskStatus() != TaskStatusWaitingSolReview || s.Exists("pending-decision") {
		return fmt.Errorf("parent fix transition requires waiting-sol-review without pending decision")
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return err
	}
	s.RecordFix()
	_, err := s.RecordParentOutcome(ParentOutcomeFix, origin)
	return err
}

func (s *StateStore) WaitForDecision() error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("wait-for-decision transition requires active task, got %s", s.TaskStatus())
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if err := s.Touch("pending-decision"); err != nil {
		return err
	}
	if err := s.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		return s.rollbackLifecycleFiles(err, pending)
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
	return s.SetTaskStatus(status)
}

func (s *StateStore) WaitForSolReview() error {
	switch s.TaskStatus() {
	case TaskStatusActive, TaskStatusWaitingDecision, TaskStatusWaitingSolReview:
		return s.SetTaskStatus(TaskStatusWaitingSolReview)
	default:
		return fmt.Errorf("wait-for-sol-review transition is invalid from %s", s.TaskStatus())
	}
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
	resume, err := s.snapshotLifecycleFile(resumeStateFile)
	if err != nil {
		return err
	}
	if err := s.ClearResumeCheckpoint(); err != nil {
		return err
	}
	if err := s.SetTaskStatus(TaskStatusActive); err != nil {
		return s.rollbackLifecycleFiles(err, resume)
	}
	return nil
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
		return err
	}
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if err := s.SaveResumeCheckpoint(checkpoint); err != nil {
		return err
	}
	if checkpoint.StopKind == ResumeStopGuardRecoverable {
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
