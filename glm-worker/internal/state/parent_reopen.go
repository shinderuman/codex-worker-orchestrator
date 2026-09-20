package state

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func (s *StateStore) ReopenAcceptedParentCompletion() error {
	completion, finding, err := s.reopenableParentCompletion()
	if err != nil {
		return err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	snapshots, err := s.snapshotReopenStateFiles()
	if err != nil {
		return err
	}
	if err := s.applyReopenTransition(*completion, snapshots); err != nil {
		return err
	}
	s.recordReopenedParentOutcome(taskID, finding.Origin, finding.Cause, *completion)
	return nil
}

func (s *StateStore) reopenableParentCompletion() (*ParentCompletionOutcome, *PublicationInvalidatingFinding, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return nil, nil, fmt.Errorf("reopen requires %s, got %s", TaskStatusAwaitingParentCompletion, s.TaskStatus())
	}
	completion, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return nil, nil, err
	}
	if completion == nil || completion.Terminal != SessionRotationTerminalAccept {
		return nil, nil, fmt.Errorf("reopen requires an accepted parent completion outcome to invalidate")
	}
	finding, err := s.CurrentPublicationInvalidatingFinding()
	if err != nil {
		return nil, nil, err
	}
	if finding == nil {
		return nil, nil, fmt.Errorf("reopen requires a durable invalidating correctness finding")
	}
	return completion, finding, nil
}

func (s *StateStore) snapshotReopenStateFiles() ([]lifecycleFileSnapshot, error) {
	names := []string{
		parentReviewStateFile,
		"task.status",
		publicationCandidateStateFile,
		runtimeInstallEvidenceFile,
		publicationReopenLineageStateFile,
		publicationInvalidatingFindingStateFile,
	}
	snapshots := make([]lifecycleFileSnapshot, 0, len(names))
	for _, name := range names {
		snapshot, err := s.snapshotLifecycleFile(name)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (s *StateStore) applyReopenTransition(completion ParentCompletionOutcome, snapshots []lifecycleFileSnapshot) error {
	candidate, err := s.LoadPublicationCandidate()
	if err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.CapturePublicationReopenLineage(candidate); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.ClearPublicationCandidate(); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.ClearRuntimeInstallEvidence(); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.ClearPublicationInvalidatingFinding(); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.openParentReviewState(string(packet.StatusNeedsSolReview), completion.Risk, ParentReviewProducer{}, false); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	return nil
}

func (s *StateStore) recordReopenedParentOutcome(taskID, origin, cause string, completion ParentCompletionOutcome) {
	reopened := ParentReviewOpenState{PacketStatus: string(packet.StatusNeedsSolReview), Risk: completion.Risk}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.recordParentOutcome(ParentOutcomeFix, origin, reopened)
		stats.ParentReviewOpen = &ParentReviewOpenState{PacketStatus: reopened.PacketStatus, Risk: reopened.Risk}
	})
	s.appendParentOutcomeEvent(taskID, ParentPhaseFix, ParentOutcomeFix, origin, cause, reopened)
}
