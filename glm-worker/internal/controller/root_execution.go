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
	authority, err := s.retiredProjectAuthority(head)
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	current, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, err
	}
	if current.ProjectSnapshotID != authority.ProjectSnapshotID {
		return head, BlockerEpisodeRevision{}, SemanticTaskRef{}, fmt.Errorf("parent metadata advancement must be adopted before ordinary start")
	}
	return head, BlockerEpisodeRevision{}, authority.Task, nil
}
