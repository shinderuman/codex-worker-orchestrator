package controller

import (
	"fmt"
	"time"
)

func planRootResumeClosure(op *ExecutionOperation, head RepositoryControllerHead, task SemanticTaskRef) error {
	if op.Episode == nil || op.Episode.EpisodeID == "" || head.RootTaskRef == nil {
		return nil
	}
	if !task.Equal(*head.RootTaskRef) || op.Episode.State != EpisodeStateResumingRoot || !taskPathSatisfied(op.Episode.SatisfiedTaskRefs, op.Episode.ScopeRootTaskRef.TaskPath) {
		return nil
	}
	closed := *op.Episode
	closed.PreviousRevisionID = closed.RevisionID
	closed.Revision++
	closed.RevisionID = ""
	closed.State = EpisodeStateClosed
	closed.SourceControllerGeneration = head.ControllerGeneration
	closed.SourceAttemptID = ""
	closed.SourceLeaseID = ""
	closed.SourceWorkspaceID = ""
	closed.SourceWorkspaceSnapshotID = ""
	closed.CreatedAt = time.Now().UTC()
	closed.RevisionID = blockerEpisodeRevisionID(closed)
	op.Episode = &closed
	op.Transition.TargetEpisodeRevision = closed.Revision
	return nil
}

func (s *Store) nextRootMaterializationAuthority(head RepositoryControllerHead) (RepositoryControllerHead, BlockerEpisodeRevision, SemanticTaskRef, error) {
	if head.MetadataLineageRef == nil || head.AcceptedCandidateRef != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, fmt.Errorf("ordinary start requires finalized terminal metadata successor authority")
	}
	record, err := s.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	if head.RootTaskRef == nil || record.ResultProject.SnapshotID != project.SnapshotID || project.HeadOID != head.IntegrationTip || !projectHasTask(project, *head.RootTaskRef) {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, fmt.Errorf("ordinary successor project authority is stale")
	}
	authority, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, head.IntegrationTip)
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	if authority.ProjectSnapshotID != project.SnapshotID || !authority.Task.Equal(*head.RootTaskRef) {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, fmt.Errorf("ordinary successor differs from canonical Plan ACTIVE")
	}
	return head, BlockerEpisodeRevision{}, *head.RootTaskRef, nil
}
