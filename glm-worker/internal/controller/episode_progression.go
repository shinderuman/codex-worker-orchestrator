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

	_, previous, _, err := s.validateEpisodeSatisfaction(input)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if !taskPathSatisfied(previous.SatisfiedTaskRefs, input.SatisfiedTaskRef.TaskPath) {
		return EpisodeScheduleResult{}, fmt.Errorf("dependency fulfillment requires controller-committed publication")
	}
	return s.scheduleEpisodeAgainstProject(previous)
}

func (s *Store) validateEpisodeSatisfaction(
	input EpisodeSatisfactionInput,
) (RepositoryControllerHead, BlockerEpisodeRevision, ProjectSnapshot, error) {
	if err := validateEpisodeSatisfactionInput(input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	head, err := s.LoadHead()
	if err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	if err := validateEpisodeSatisfactionHead(head, input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	project, err := s.LoadProjectSnapshot(input.ProjectSnapshotID)
	if err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	previous, err := s.LoadEpisodeRevision(input.EpisodeID, input.ExpectedRevision)
	if err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	if err := validateEpisodeSatisfactionRevision(head, previous, project, input); err != nil {
		return RepositoryControllerHead{}, BlockerEpisodeRevision{}, ProjectSnapshot{}, err
	}
	return head, previous, project, nil
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
	project ProjectSnapshot,
	input EpisodeSatisfactionInput,
) error {
	if previous.State == EpisodeStateClosed {
		return fmt.Errorf("closed blocker episode cannot accept satisfaction")
	}
	if head.RootTaskRef == nil || head.RootTaskRef.TaskPath != previous.RootTaskRef.TaskPath {
		return fmt.Errorf("episode satisfaction root authority does not match controller")
	}
	if !projectHasTask(project, *head.RootTaskRef) {
		return fmt.Errorf("episode satisfaction root authority is not in result project snapshot")
	}
	if !taskRefIn(previous.AdmittedClosure, input.SatisfiedTaskRef) {
		return fmt.Errorf("satisfied task is outside admitted blocker closure")
	}
	return nil
}

func progressedEpisodeRevision(
	previous BlockerEpisodeRevision,
	head RepositoryControllerHead,
	project ProjectSnapshot,
	input EpisodeSatisfactionInput,
) (BlockerEpisodeRevision, error) {
	refs := projectRefsByPath(project)
	satisfied := append([]SemanticTaskRef(nil), previous.SatisfiedTaskRefs...)
	if !input.SatisfiedTaskRef.Empty() {
		satisfied = appendTaskPathUnique(satisfied, input.SatisfiedTaskRef)
	}
	next, err := rebindProgressedEpisode(previous, refs, satisfied)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	next.Revision = previous.Revision + 1
	next.RevisionID = ""
	next.PreviousRevisionID = previous.RevisionID
	next.ProjectSnapshotID = project.SnapshotID
	next.RootTaskRef = *head.RootTaskRef
	next.TriggerFindingID = ""
	next.SatisfiedTaskRefs = satisfied
	next.SourceControllerGeneration = head.ControllerGeneration
	next.SourceAttemptID = ""
	next.SourceLeaseID = ""
	next.SourceWorkspaceID = ""
	next.SourceWorkspaceSnapshotID = ""
	next.State = EpisodeStateReplanning
	if taskPathSatisfied(next.SatisfiedTaskRefs, next.ScopeRootTaskRef.TaskPath) {
		next.State = EpisodeStateResumingRoot
	}
	next.CreatedAt = time.Now().UTC()
	next.RevisionID = blockerEpisodeRevisionID(next)
	return next, nil
}

func rebindProgressedEpisode(
	previous BlockerEpisodeRevision,
	refs map[string]SemanticTaskRef,
	satisfied []SemanticTaskRef,
) (BlockerEpisodeRevision, error) {
	next := previous
	var err error
	next.ScopeRootTaskRef, err = rebindEpisodeRef(previous.ScopeRootTaskRef, refs, satisfied)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	next.DependencyEdges, err = rebindEpisodeEdges(previous.DependencyEdges, refs, satisfied)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	next.AdmittedClosure, err = rebindEpisodeRefs(previous.AdmittedClosure, refs, satisfied)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	next.AdmittedOrder, err = rebindEpisodeRefs(previous.AdmittedOrder, refs, satisfied)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	next.ExecutionHistory = append([]SemanticTaskRef(nil), previous.ExecutionHistory...)
	return next, nil
}

func rebindEpisodeEdges(
	edges []EpisodeDependencyEdge,
	refs map[string]SemanticTaskRef,
	satisfied []SemanticTaskRef,
) ([]EpisodeDependencyEdge, error) {
	result := make([]EpisodeDependencyEdge, 0, len(edges))
	for _, edge := range edges {
		blocked, err := rebindEpisodeRef(edge.BlockedTaskRef, refs, satisfied)
		if err != nil {
			return nil, err
		}
		dependency, err := rebindEpisodeRef(edge.DependencyTaskRef, refs, satisfied)
		if err != nil {
			return nil, err
		}
		edge.BlockedTaskRef = blocked
		edge.DependencyTaskRef = dependency
		result = append(result, edge)
	}
	return result, nil
}

func rebindEpisodeRefs(
	previous []SemanticTaskRef,
	refs map[string]SemanticTaskRef,
	satisfied []SemanticTaskRef,
) ([]SemanticTaskRef, error) {
	result := make([]SemanticTaskRef, 0, len(previous))
	for _, ref := range previous {
		rebound, err := rebindEpisodeRef(ref, refs, satisfied)
		if err != nil {
			return nil, err
		}
		result = append(result, rebound)
	}
	return result, nil
}

func rebindEpisodeRef(
	previous SemanticTaskRef,
	refs map[string]SemanticTaskRef,
	satisfied []SemanticTaskRef,
) (SemanticTaskRef, error) {
	if current, ok := refs[previous.TaskPath]; ok {
		return current, nil
	}
	if taskPathSatisfied(satisfied, previous.TaskPath) {
		return previous, nil
	}
	return SemanticTaskRef{}, fmt.Errorf("unsatisfied blocker task %s is missing from result project snapshot", previous.TaskPath)
}

func projectRefsByPath(project ProjectSnapshot) map[string]SemanticTaskRef {
	refs := make(map[string]SemanticTaskRef, len(project.Tasks))
	for _, ref := range project.Tasks {
		refs[ref.TaskPath] = ref
	}
	return refs
}

func appendTaskPathUnique(refs []SemanticTaskRef, candidate SemanticTaskRef) []SemanticTaskRef {
	if taskPathSatisfied(refs, candidate.TaskPath) {
		return refs
	}
	return append(refs, candidate)
}
