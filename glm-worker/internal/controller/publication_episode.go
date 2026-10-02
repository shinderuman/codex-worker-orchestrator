package controller

func (s *Store) rebindPublicationEpisode(op *ExecutionOperation, head RepositoryControllerHead, project ProjectSnapshot) error {
	if head.ActiveEpisodeID == "" {
		return nil
	}
	previous, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		return err
	}
	next, err := rebindProgressedEpisode(previous, projectRefsByPath(project), previous.SatisfiedTaskRefs)
	if err != nil {
		return err
	}
	next.Revision = previous.Revision + 1
	next.RevisionID = ""
	next.PreviousRevisionID = previous.RevisionID
	next.ProjectSnapshotID = project.SnapshotID
	next.SourceControllerGeneration = head.ControllerGeneration
	next.CreatedAt = op.Transition.CreatedAt
	next.RevisionID = blockerEpisodeRevisionID(next)
	op.Publication.Episode = &next
	op.Transition.TargetEpisodeRevision = next.Revision
	return nil
}

func (s *Store) planPublicationAdvancementOwnership(op *ExecutionOperation, head RepositoryControllerHead, project ProjectSnapshot) error {
	if err := s.rebindPublicationEpisode(op, head, project); err != nil {
		return err
	}
	return s.planAdvancementLocalRef(op, head)
}
