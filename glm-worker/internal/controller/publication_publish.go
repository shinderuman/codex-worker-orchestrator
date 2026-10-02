package controller

import "fmt"

func (s *Store) PublishAcceptedCandidate(input PublicationInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if !c.EvidenceValid || c.State != candidatePromoted {
		return ExecutionOperationResult{}, fmt.Errorf("publication requires promoted candidate with current evidence")
	}
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.validateCandidatePublicationRemote(c, remote); err != nil {
		return ExecutionOperationResult{}, err
	}
	project, err := s.publicationProjectAdvancement(head, c.CommitOID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	c.ProjectSnapshotID = project.SnapshotID
	c.State = candidateObserved
	c.ObservedRemoteOID = c.CommitOID
	op, err := s.planCandidateRevision(head, c, ref, publicationPublish)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.RemoteOID = remote
	op.Publication.NewTip = c.CommitOID
	op.Publication.Project = &project
	op.Transition.ProjectSnapshotNew = project.SnapshotID
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceHistory, Resource: c.Policy.Remote + ":" + c.Policy.RemoteRef, ExpectedOld: c.BaseOID, ExpectedNew: c.CommitOID}}
	if err := s.planPublishedEpisode(&op, head, c); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) applyCandidatePublication(op ExecutionOperation, c AcceptedCandidate) error {
	remote, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return err
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return err
	}
	if !contains {
		remote, err = s.pushAndObserveCandidate(op, c, remote)
		if err != nil {
			return err
		}
	}
	return s.commitPublicationOperation(op, c, remote)
}

func (s *Store) planPublishedEpisode(op *ExecutionOperation, head RepositoryControllerHead, c AcceptedCandidate) error {
	if c.EpisodeID == "" {
		return nil
	}
	previous, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		return err
	}
	if op.Publication.Project == nil {
		return fmt.Errorf("publication result project authority is missing")
	}
	project := *op.Publication.Project
	next, err := progressedEpisodeRevision(previous, head, project, EpisodeSatisfactionInput{SatisfiedTaskRef: c.TaskRef})
	if err != nil {
		return err
	}
	op.Publication.Episode = &next
	op.Transition.TargetEpisodeRevision = next.Revision
	return nil
}

func (s *Store) abortRacedPublication(op ExecutionOperation, remote string) error {
	head, err := s.LoadHead()
	if err != nil {
		return err
	}
	if err := s.ensurePublicationPrefix(head, remote); err != nil {
		return err
	}
	if remote == op.Publication.RemoteOID {
		return fmt.Errorf("publication did not apply; retained for retry")
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, op.Publication.RemoteOID, remote)
	if err != nil {
		return err
	}
	if !descendant {
		return fmt.Errorf("publication remote movement is outside race authority")
	}
	phase, err := s.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return err
	}
	if head.ControllerGeneration != op.Transition.PreparedGeneration || head.PendingTransitionID != op.Transition.TransitionID {
		return fmt.Errorf("publication race no longer owns controller")
	}
	phase.Phase = TransitionPhaseAborted
	phase.Observed = map[string]string{op.Transition.Effects[0].Key(): remote}
	if err := s.writeTransitionState(phase); err != nil {
		return err
	}
	_, err = s.finishPublicationAbort(op, head, phase)
	return err
}

func (s *Store) pushAndObserveCandidate(op ExecutionOperation, c AcceptedCandidate, remote string) (string, error) {
	if remote != op.Publication.RemoteOID {
		return "", s.abortRacedPublication(op, remote)
	}
	_, pushErr := runGitBinary(s.identity.PrimaryRoot, nil, "push", "--porcelain", c.Policy.Remote, c.CommitOID+":"+c.Policy.RemoteRef)
	observed, err := observePublicationRemote(s.identity.PrimaryRoot, c.Policy)
	if err != nil {
		return "", fmt.Errorf("publication result is unresolved: %w", err)
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, observed)
	if err != nil {
		return "", err
	}
	if !contains {
		if observed == remote && pushErr != nil {
			return "", fmt.Errorf("publication did not apply; retained for retry: %w", pushErr)
		}
		return "", s.abortRacedPublication(op, observed)
	}
	return observed, nil
}

func (s *Store) validateCandidatePublicationRemote(c AcceptedCandidate, remote string) error {
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return err
	}
	if remote != c.BaseOID && !contains {
		return fmt.Errorf("unpublished candidate requires external advancement rebind")
	}
	return nil
}
