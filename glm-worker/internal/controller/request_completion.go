package controller

import (
	"fmt"
	"reflect"
)

func (s *Store) terminalResultAuthority(candidate AcceptedCandidate, source ProjectSnapshot) (TaskAuthority, error) {
	if candidate.TaskRef.RequestID == "" {
		return ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, candidate.CommitOID)
	}
	if source.UserRequest == nil || !projectHasTask(source, candidate.TaskRef) || len(candidate.TerminalFiles) != 0 {
		return TaskAuthority{}, fmt.Errorf("request completion has project-only metadata or stale authority")
	}
	result := source
	result.HeadOID = candidate.CommitOID
	result.CompletedRequest = source.UserRequest
	result.UserRequest = nil
	result.Tasks = nil
	result.TaskCorpusDigest = digestTaskRefs(nil)
	result.SnapshotID = projectSnapshotID(result)
	return TaskAuthority{ProjectSnapshotID: result.SnapshotID, Snapshot: result}, nil
}

func validateTerminalRequestBinding(record TerminalTaskRecord, next *SemanticTaskRef) error {
	if next != nil || len(record.Bindings) != 0 || record.SourceProject.UserRequest == nil || record.ResultProject.CompletedRequest == nil {
		return fmt.Errorf("request completion contains a project successor or incomplete authority")
	}
	for _, snapshot := range []ProjectSnapshot{record.SourceProject, record.ResultProject} {
		if snapshot.SchemaVersion != controllerSchemaVersion || snapshot.RepositoryIdentity != record.RepositoryIdentity {
			return fmt.Errorf("request completion project identity differs")
		}
		if err := validateRequestTaskSnapshot(snapshot); err != nil {
			return err
		}
	}
	if !projectHasTask(record.SourceProject, record.TaskRef) || projectHasTask(record.ResultProject, record.TaskRef) || !reflect.DeepEqual(record.SourceProject.UserRequest, record.ResultProject.CompletedRequest) {
		return fmt.Errorf("request completion changes immutable request authority")
	}
	return nil
}

func (s *Store) requireCompletedRequest(head RepositoryControllerHead) error {
	if !completedRequestHeadReady(head) {
		return fmt.Errorf("request bootstrap requires pristine or finalized request authority")
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return err
	}
	if project.CompletedRequest == nil || project.HeadOID != head.IntegrationTip {
		return fmt.Errorf("previous authority is not a completed request")
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return err
	}
	if record.ResultProject.SnapshotID != project.SnapshotID {
		return fmt.Errorf("completed request lineage differs")
	}
	_, phase, err := s.LoadTransition(record.TransitionID)
	if err != nil {
		return err
	}
	if phase.Phase != TransitionPhaseFinalized {
		return fmt.Errorf("previous request completion is unfinished")
	}
	attempt, err := s.loadAttempt(record.AttemptID)
	if err != nil {
		return err
	}
	runtime, _, err := s.boundAttemptRuntime(Admission{Attempt: attempt})
	if err != nil {
		return err
	}
	if runtime == nil {
		return fmt.Errorf("completed request has no acknowledged Runtime")
	}
	return validateCompletedRuntime(runtime)
}

func (s *Store) completedRequestTask(head RepositoryControllerHead) (SemanticTaskRef, bool, error) {
	if head.ProjectSnapshotID == "" {
		return SemanticTaskRef{}, false, nil
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return SemanticTaskRef{}, false, err
	}
	if project.CompletedRequest == nil {
		return SemanticTaskRef{}, false, nil
	}
	if head.MetadataLineageRef == nil {
		return SemanticTaskRef{}, true, fmt.Errorf("completed request lineage is missing")
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	if record.ResultProject.SnapshotID != project.SnapshotID {
		return SemanticTaskRef{}, true, fmt.Errorf("completed request read authority differs")
	}
	return record.TaskRef, true, nil
}

func (s *Store) requestCompletionHasNoSuccessor(head RepositoryControllerHead) error {
	if head.MetadataLineageRef == nil {
		return fmt.Errorf("quiescent Runtime requires selected Task authority")
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return err
	}
	if record.ResultProject.CompletedRequest == nil || record.ResultProject.SnapshotID != head.ProjectSnapshotID || head.RootTaskRef != nil || head.ExecutionTaskRef != nil {
		return fmt.Errorf("request completion has unexpected successor authority")
	}
	return nil
}

func (s *Store) completionTargetAuthority(op ExecutionOperation) (TaskAuthority, error) {
	if op.Terminal.Record.TaskRef.RequestID == "" {
		return ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	}
	if err := validateTerminalRequestBinding(op.Terminal.Record, op.Terminal.RootTaskRef); err != nil {
		return TaskAuthority{}, err
	}
	project := op.Terminal.Record.ResultProject
	actual, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return TaskAuthority{}, err
	}
	if actual != project.HeadOID {
		return TaskAuthority{}, fmt.Errorf("request completion HEAD differs from target authority")
	}
	return TaskAuthority{ProjectSnapshotID: project.SnapshotID, Snapshot: project}, nil
}

func completionRootMatches(root *SemanticTaskRef, authority TaskAuthority) bool {
	if authority.Snapshot.CompletedRequest != nil {
		return root == nil && authority.Task.Empty()
	}
	return root != nil && root.Equal(authority.Task)
}

func completedRequestHeadReady(head RepositoryControllerHead) bool {
	return head.Status == ControllerStatusActive && head.RootTaskRef == nil && head.ExecutionTaskRef == nil && head.LiveAttemptID == "" && head.LiveLeaseID == "" && head.PendingTransitionID == "" && head.AcceptedCandidateRef == nil && head.MetadataLineageRef != nil && head.ActiveEpisodeID == ""
}
