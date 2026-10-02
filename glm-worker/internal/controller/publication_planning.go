package controller

import "fmt"

func (s *Store) PromoteAcceptedCandidate(input PublicationInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if !c.EvidenceValid {
		return ExecutionOperationResult{}, fmt.Errorf("candidate evidence requires re-entry")
	}
	if c.State == candidateObserved {
		return ExecutionOperationResult{}, fmt.Errorf("observed candidate cannot be locally rewritten")
	}
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	local, err := s.candidatePromotionLocal(c)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if c.State == candidatePromoted && local == c.CommitOID {
		return ExecutionOperationResult{Head: head, CandidateRef: &ref}, nil
	}
	c.State = candidatePromoted
	op, err := s.planCandidateRevision(head, c, ref, publicationPromote)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Publication.LocalOld = local
	op.Publication.RemoteOID = remote
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: c.Policy.LocalRef, ExpectedOld: local, ExpectedNew: c.CommitOID}}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) publicationCandidateAuthority(input PublicationInput) (RepositoryControllerHead, AcceptedCandidate, EvidenceObjectRef, error) {
	head, err := s.quiescentExecutionHead(input.ExpectedGeneration)
	if err != nil {
		return head, AcceptedCandidate{}, EvidenceObjectRef{}, err
	}
	if head.AcceptedCandidateRef == nil {
		return head, AcceptedCandidate{}, EvidenceObjectRef{}, fmt.Errorf("controller has no accepted candidate")
	}
	c, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return head, c, EvidenceObjectRef{}, err
	}
	if c.CandidateID != input.CandidateID || c.ProjectSnapshotID != head.ProjectSnapshotID {
		return head, c, EvidenceObjectRef{}, fmt.Errorf("candidate authority is stale")
	}
	return head, c, *head.AcceptedCandidateRef, nil
}

func (s *Store) planCandidateRevision(head RepositoryControllerHead, c AcceptedCandidate, previous EvidenceObjectRef, kind string) (ExecutionOperation, error) {
	record, err := executionTransition(head, kind)
	if err != nil {
		return ExecutionOperation{}, err
	}
	c.Previous = &previous
	c.ControllerGeneration = record.CommittedGeneration
	c.TransitionID = record.TransitionID
	c.CreatedAt = record.CreatedAt
	ref, err := s.storeAcceptedCandidate(c)
	if err != nil {
		return ExecutionOperation{}, err
	}
	op := ExecutionOperation{Transition: record, Publication: &PublicationOperation{Before: &previous, After: &ref, Policy: c.Policy, OldTip: head.IntegrationTip, NewTip: head.IntegrationTip, RemoteOID: head.ObservedRemoteTip}, SealRef: &c.SealRef}
	return op, nil
}

func (s *Store) RevalidateAcceptedCandidate(input PublicationInput, evidence []EvidenceObjectRef) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	head, c, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if c.State == candidateObserved {
		if c.DescendantTip == "" || c.DescendantTip != head.IntegrationTip {
			return ExecutionOperationResult{}, fmt.Errorf("observed descendant validation authority is incomplete")
		}
		tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", head.IntegrationTip+"^{tree}")
		if err != nil {
			return ExecutionOperationResult{}, err
		}
		if err := s.validateCandidateEvidence(canonicalDescendantEvidenceView(c, tree), evidence); err != nil {
			return ExecutionOperationResult{}, err
		}
		c.DescendantEvidence = evidence
	} else {
		if err := s.validateCandidateEvidence(c, evidence); err != nil {
			return ExecutionOperationResult{}, err
		}
		c.Evidence = evidence
		c.EvidenceValid = true
	}
	op, err := s.planCandidateRevision(head, c, ref, publicationRevalidate)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceState, Resource: "accepted-candidate", ExpectedOld: ref.Digest, ExpectedNew: op.Publication.After.Digest}}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}
