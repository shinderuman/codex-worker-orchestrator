package controller

import "fmt"

func (s *Store) AdoptExternalAdvancement(input ExternalAdvancementInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if head.AcceptedCandidateRef == nil {
		return s.planSuspendedExternalAdvancement(head, input.Policy)
	}
	c, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := validateObservedAdvancementCandidate(c, input.Policy); err != nil {
		return ExecutionOperationResult{}, err
	}
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	project, err := s.publicationProjectAdvancement(head, remote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	c.ProjectSnapshotID = project.SnapshotID
	if remote != c.CommitOID {
		c.DescendantTip = remote
		c.DescendantEvidence = nil
	}
	op, err := s.planCandidateRevision(head, c, *head.AcceptedCandidateRef, publicationAdopt)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.NewTip = remote
	op.Publication.RemoteOID = remote
	op.Publication.Project = &project
	op.Transition.ProjectSnapshotNew = project.SnapshotID
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "accepted-candidate", ExpectedOld: head.AcceptedCandidateRef.Digest, ExpectedNew: op.Publication.After.Digest}}
	if err := s.planPublicationAdvancementOwnership(&op, head, project); err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func canonicalDescendantEvidenceView(c AcceptedCandidate, tree string) AcceptedCandidate {
	c.BaseOID = c.DescendantTip
	c.TreeOID = tree
	c.SnapshotID = digestStrings("canonical-descendant", c.CandidateID, c.DescendantTip, tree)
	return c
}

func validateObservedAdvancementCandidate(c AcceptedCandidate, policy PublicationPolicy) error {
	if c.Policy != policy {
		return fmt.Errorf("external advancement publication policy differs")
	}
	if c.State != candidateObserved {
		return fmt.Errorf("unpublished candidate requires candidate rebind")
	}
	return nil
}
