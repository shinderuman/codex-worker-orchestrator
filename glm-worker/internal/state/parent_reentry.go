package state

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ControllerReentryAuthority struct {
	PredecessorTaskContractDigest string
	PredecessorAttemptID          string
	AttemptID                     string
	TaskPath                      string
	TaskContractDigest            string
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
	if authority.TaskContractDigest == "" || authority.PredecessorTaskContractDigest == "" || binding.TaskPath != authority.TaskPath || binding.TaskContractDigest != authority.PredecessorTaskContractDigest {
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
	if err := s.openParentReviewState(string(packet.StatusNeedsSolReview), risk, ParentReviewProducer{}); err != nil {
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

func (s *StateStore) EnterReenteredReviewStop(checkpoint ResumeCheckpoint) error {
	if err := s.admitReenteredReviewStop(checkpoint); err != nil {
		return err
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	if _, _, err := s.resolveParentReviewState(ParentOutcomeFix, ParentOriginUserAmendment, ParentCauseSolGate); err != nil {
		return err
	}
	if err := s.EnterStop(checkpoint); err != nil {
		return s.rollbackLifecycleFiles(err, review)
	}
	return nil
}

func (s *StateStore) admitReenteredReviewStop(checkpoint ResumeCheckpoint) error {
	if s.TaskStatus() != TaskStatusWaitingSolReview || checkpoint.Stage != ResumeStageReview || checkpoint.Role != ReviewerRole || !checkpoint.ReadOnly || checkpoint.ResultCorrection || checkpoint.StopKind != ResumeStopInterrupted {
		return fmt.Errorf("reentered review stop requires its normal read-only Reviewer contract")
	}
	open, err := s.CurrentParentReview()
	if err != nil {
		return err
	}
	if open == nil || open.Role != "" || open.ModelAlias != "" || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return fmt.Errorf("reentered review stop cannot resolve an actual model review")
	}
	return nil
}

func (s *StateStore) ReenteredReviewStopRepairable() (bool, error) {
	open, err := s.CurrentParentReview()
	if err != nil {
		return false, err
	}
	if open == nil || open.Role != "" || open.ModelAlias != "" || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		return false, nil
	}
	return s.ReenteredReviewerStopRetained()
}

func (s *StateStore) ReenteredReviewerStopRetained() (bool, error) {
	if s.TaskStatus() != TaskStatusInterrupted {
		return false, nil
	}
	checkpoint, err := s.LoadResumeCheckpoint()
	if err != nil {
		return false, err
	}
	if !reenteredReviewerCheckpoint(checkpoint) {
		return false, nil
	}
	workerEnd, err := s.LoadWorkerEndSnapshot()
	if err != nil {
		return false, err
	}
	return checkpoint.StopGitSnapshot != nil && EqualGitSnapshot(workerEnd, *checkpoint.StopGitSnapshot), nil
}

func reenteredReviewerCheckpoint(checkpoint ResumeCheckpoint) bool {
	return checkpoint.Stage == ResumeStageReview && checkpoint.Role == ReviewerRole && checkpoint.ReadOnly && !checkpoint.ResultCorrection && !checkpoint.ReportOnly && checkpoint.StopKind == ResumeStopInterrupted && checkpoint.WorkerResult != nil
}

func (s *StateStore) ReplaceReenteredReviewStop(source, target ResumeCheckpoint) error {
	repairable, err := s.ReenteredReviewStopRepairable()
	if err != nil {
		return err
	}
	if !repairable || !reenteredReviewerCheckpoint(target) {
		return fmt.Errorf("reentered review repair requires its retained read-only checkpoint")
	}
	saved, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(saved, source) {
		return fmt.Errorf("reentered review repair checkpoint changed")
	}
	if target.StopGitSnapshot == nil {
		return fmt.Errorf("review repair lacks its new snapshot")
	}
	return s.replaceReenteredReviewStop(target)
}

func (s *StateStore) UpdateReenteredReviewStop(source, target ResumeCheckpoint) error {
	open, err := s.CurrentParentReview()
	if err != nil {
		return err
	}
	retained, err := s.ReenteredReviewerStopRetained()
	if err != nil {
		return err
	}
	if open != nil || !retained || !reenteredReviewerCheckpoint(target) || target.StopGitSnapshot == nil {
		return fmt.Errorf("review update requires a retained queued read-only Reviewer")
	}
	saved, err := s.LoadResumeCheckpoint()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(saved, source) {
		return fmt.Errorf("review update checkpoint changed")
	}
	return s.replaceReenteredReviewSnapshots(target)
}

func (s *StateStore) replaceReenteredReviewStop(target ResumeCheckpoint) error {
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	workerEnd, err := s.snapshotLifecycleFile(workerEndSnapshotFile)
	if err != nil {
		return err
	}
	reviewStart, err := s.snapshotLifecycleFile(reviewStartSnapshotFile)
	if err != nil {
		return err
	}
	if _, _, err := s.resolveParentReviewState(ParentOutcomeFix, ParentOriginUserAmendment, ParentCauseSolGate); err != nil {
		return err
	}
	if err := s.replaceReenteredReviewSnapshots(target); err != nil {
		return s.rollbackLifecycleFiles(err, review, workerEnd, reviewStart)
	}
	return nil
}

func (s *StateStore) replaceReenteredReviewSnapshots(target ResumeCheckpoint) error {
	workerEnd, err := s.snapshotLifecycleFile(workerEndSnapshotFile)
	if err != nil {
		return err
	}
	reviewStart, err := s.snapshotLifecycleFile(reviewStartSnapshotFile)
	if err != nil {
		return err
	}
	if err := s.SaveWorkerEndSnapshot(*target.StopGitSnapshot); err != nil {
		return s.rollbackLifecycleFiles(err, workerEnd, reviewStart)
	}
	if err := s.SaveReviewStartSnapshot(*target.StopGitSnapshot); err != nil {
		return s.rollbackLifecycleFiles(err, workerEnd, reviewStart)
	}
	if err := s.EnterStop(target); err != nil {
		return s.rollbackLifecycleFiles(err, workerEnd, reviewStart)
	}
	return nil
}

func (s *StateStore) ReviseParentSourceReviewCheckpoint(source ParentReviewOpenState, target ResumeCheckpoint) error {
	if s.TaskStatus() != TaskStatusWaitingSolReview || source.Role != "reviewer" || source.PacketStatus != string(packet.StatusNeedsSolReview) || !reenteredReviewerCheckpoint(target) || target.StopGitSnapshot == nil {
		return fmt.Errorf("source revision requires its actual independent Review and read-only continuation")
	}
	open, err := s.CurrentParentReview()
	if err != nil {
		return err
	}
	if open == nil || !reflect.DeepEqual(*open, source) {
		return fmt.Errorf("parent source review producer changed")
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	if _, _, err := s.resolveParentReviewState(ParentOutcomeFix, ParentOriginCodexReview, ParentCauseTestScenario); err != nil {
		return err
	}
	if err := s.replaceReenteredReviewSnapshots(target); err != nil {
		return s.rollbackLifecycleFiles(err, review)
	}
	return nil
}
