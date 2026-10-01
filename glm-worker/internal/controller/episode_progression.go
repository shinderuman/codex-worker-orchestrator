package controller

import (
	"fmt"
	"time"
)

type EpisodeSatisfactionInput struct {
	EpisodeID                    string          `json:"episode_id"`
	ExpectedRevision             uint64          `json:"expected_revision"`
	ExpectedControllerGeneration uint64          `json:"expected_controller_generation"`
	ProjectSnapshotID            string          `json:"project_snapshot_id"`
	SatisfiedTaskRef             SemanticTaskRef `json:"satisfied_task_ref"`
}

func (s *Store) SatisfyEpisodeTask(input EpisodeSatisfactionInput) (EpisodeScheduleResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	defer func() { _ = lock.Close() }()

	head, previous, err := s.validateEpisodeSatisfaction(input)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if taskRefIn(previous.SatisfiedTaskRefs, input.SatisfiedTaskRef) {
		return scheduleEpisodeRevision(previous), nil
	}

	next := progressedEpisodeRevision(previous, head, input)
	if err := s.writeEpisodeRevision(next); err != nil {
		return EpisodeScheduleResult{}, err
	}
	return scheduleEpisodeRevision(next), nil
}

func (s *Store) validateEpisodeSatisfaction(
	input EpisodeSatisfactionInput,
) (RepositoryControllerHead, BlockerEpisodeRevision, error) {
	if err := validateEpisodeSatisfactionInput(input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	if err := validateEpisodeSatisfactionHead(head, input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	if _, err := s.LoadProjectSnapshot(input.ProjectSnapshotID); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	previous, err := s.LoadEpisodeRevision(input.EpisodeID, input.ExpectedRevision)
	if err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	if err := validateEpisodeSatisfactionRevision(head, previous, input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, err
	}
	return head, previous, nil
}

func validateEpisodeSatisfactionInput(input EpisodeSatisfactionInput) error {
	if input.EpisodeID == "" || input.ExpectedRevision == 0 || input.ProjectSnapshotID == "" || input.SatisfiedTaskRef.Empty() {
		return fmt.Errorf("episode satisfaction authority is incomplete")
	}
	return nil
}

func validateEpisodeSatisfactionHead(head RepositoryControllerHead, input EpisodeSatisfactionInput) error {
	if head.Status != ControllerStatusActive {
		return fmt.Errorf("episode satisfaction requires active controller authority")
	}
	if head.ControllerGeneration != input.ExpectedControllerGeneration {
		return fmt.Errorf(
			"episode satisfaction controller generation is stale: got=%d want=%d",
			input.ExpectedControllerGeneration,
			head.ControllerGeneration,
		)
	}
	if head.ProjectSnapshotID != input.ProjectSnapshotID {
		return fmt.Errorf("episode satisfaction project snapshot is stale")
	}
	if head.ActiveEpisodeID != input.EpisodeID || head.ActiveEpisodeRevision != input.ExpectedRevision {
		return fmt.Errorf("episode satisfaction revision is stale")
	}
	return nil
}

func validateEpisodeSatisfactionRevision(
	head RepositoryControllerHead,
	previous BlockerEpisodeRevision,
	input EpisodeSatisfactionInput,
) error {
	if previous.State == EpisodeStateClosed {
		return fmt.Errorf("closed blocker episode cannot accept satisfaction")
	}
	if head.RootTaskRef == nil || !head.RootTaskRef.Equal(previous.RootTaskRef) {
		return fmt.Errorf("episode satisfaction root authority does not match controller")
	}
	if !taskRefIn(previous.AdmittedClosure, input.SatisfiedTaskRef) {
		return fmt.Errorf("satisfied task is outside admitted blocker closure")
	}
	return nil
}

func progressedEpisodeRevision(
	previous BlockerEpisodeRevision,
	head RepositoryControllerHead,
	input EpisodeSatisfactionInput,
) BlockerEpisodeRevision {
	next := previous
	next.Revision = previous.Revision + 1
	next.RevisionID = ""
	next.PreviousRevisionID = previous.RevisionID
	next.ProjectSnapshotID = input.ProjectSnapshotID
	next.TriggerFindingID = ""
	next.DependencyEdges = append([]EpisodeDependencyEdge(nil), previous.DependencyEdges...)
	next.SatisfiedTaskRefs = appendTaskRefUnique(
		append([]SemanticTaskRef(nil), previous.SatisfiedTaskRefs...),
		input.SatisfiedTaskRef,
	)
	next.ExecutionHistory = append([]SemanticTaskRef(nil), previous.ExecutionHistory...)
	next.AdmittedClosure = append([]SemanticTaskRef(nil), previous.AdmittedClosure...)
	next.AdmittedOrder = append([]SemanticTaskRef(nil), previous.AdmittedOrder...)
	next.SourceControllerGeneration = head.ControllerGeneration
	next.SourceAttemptID = ""
	next.SourceLeaseID = ""
	next.SourceWorkspaceID = ""
	next.SourceWorkspaceSnapshotID = ""
	next.State = EpisodeStateReplanning
	if taskRefIn(next.SatisfiedTaskRefs, next.ScopeRootTaskRef) {
		next.State = EpisodeStateResumingRoot
	}
	next.CreatedAt = time.Now().UTC()
	next.RevisionID = blockerEpisodeRevisionID(next)
	return next
}
