package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func (s *StateStore) ReopenAcceptedParentCompletion(origin, cause string) error {
	completion, err := s.reopenableParentCompletion(origin, cause)
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
	s.recordReopenedParentOutcome(taskID, origin, cause, *completion)
	return nil
}

func (s *StateStore) reopenableParentCompletion(origin, cause string) (*ParentCompletionOutcome, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return nil, fmt.Errorf("reopen requires %s, got %s", TaskStatusAwaitingParentCompletion, s.TaskStatus())
	}
	if err := validateParentFixDeclaration(origin, cause); err != nil {
		return nil, err
	}
	completion, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return nil, err
	}
	if completion == nil || completion.Terminal != SessionRotationTerminalAccept {
		return nil, fmt.Errorf("reopen requires an accepted parent completion outcome to invalidate")
	}
	return completion, nil
}

func (s *StateStore) snapshotReopenStateFiles() ([]lifecycleFileSnapshot, error) {
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return nil, err
	}
	status, err := s.snapshotLifecycleFile("task.status")
	if err != nil {
		return nil, err
	}
	candidate, err := s.snapshotLifecycleFile(publicationCandidateStateFile)
	if err != nil {
		return nil, err
	}
	evidence, err := s.snapshotLifecycleFile(runtimeInstallEvidenceFile)
	if err != nil {
		return nil, err
	}
	lineage, err := s.snapshotLifecycleFile(publicationReopenLineageStateFile)
	if err != nil {
		return nil, err
	}
	return []lifecycleFileSnapshot{review, status, candidate, evidence, lineage}, nil
}

func (s *StateStore) applyReopenTransition(completion ParentCompletionOutcome, snapshots []lifecycleFileSnapshot) error {
	candidate, err := s.LoadPublicationCandidate()
	if err == nil {
		if err := s.CapturePublicationReopenLineage(candidate); err != nil {
			return s.rollbackLifecycleFiles(err, snapshots...)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.ClearPublicationCandidate(); err != nil {
		return s.rollbackLifecycleFiles(err, snapshots...)
	}
	if err := s.ClearRuntimeInstallEvidence(); err != nil {
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
