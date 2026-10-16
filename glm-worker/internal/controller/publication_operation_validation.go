package controller

import "fmt"

func (s *Store) validatePublicationOperation(op ExecutionOperation) error {
	if isBaseAdvancementOperation(op) {
		return s.validateBaseAdvancement(op)
	}
	if op.Publication == nil || op.Publication.After == nil {
		return fmt.Errorf("publication operation authority is missing")
	}
	candidate, err := s.LoadAcceptedCandidate(*op.Publication.After)
	if err != nil {
		return err
	}
	if candidate.ControllerGeneration != op.Transition.CommittedGeneration || candidate.TransitionID != op.Transition.TransitionID || candidate.Policy != op.Publication.Policy {
		return fmt.Errorf("candidate and publication transition differ")
	}
	if err := s.validatePublicationPrimaryArchive(op, candidate); err != nil {
		return err
	}
	if op.Publication.Before != nil {
		if _, err := s.LoadAcceptedCandidate(*op.Publication.Before); err != nil {
			return err
		}
	}
	return s.validatePublicationCandidateBinding(op, candidate)
}

func isBaseAdvancementOperation(op ExecutionOperation) bool {
	return op.Publication != nil && op.Publication.After == nil && op.Transition.Kind == publicationAdopt
}

func (s *Store) validatePublicationPrimaryArchive(op ExecutionOperation, candidate AcceptedCandidate) error {
	if op.Transition.Kind == publicationPromote {
		return s.validatePrimaryPromotion(op, candidate)
	}
	if op.Publication.PrimaryArchive != nil {
		return fmt.Errorf("primary promotion archive is outside promotion authority")
	}
	return nil
}

func (s *Store) verifyCommittedPublication(op ExecutionOperation, head RepositoryControllerHead) error {
	if op.Publication.After == nil {
		return verifyCommittedBaseAdvancement(op, head)
	}
	if err := validateCommittedPublicationHead(op, head); err != nil {
		return err
	}
	candidate, err := s.LoadAcceptedCandidate(*op.Publication.After)
	if err != nil {
		return err
	}
	if _, err := s.provePublicationEffects(op, candidate); err != nil {
		return err
	}
	return s.verifyPublicationRemoteContainment(candidate)
}

func validateCommittedPublicationHead(op ExecutionOperation, head RepositoryControllerHead) error {
	if head.IntegrationTip != op.Publication.NewTip || head.LiveLeaseID != "" {
		return fmt.Errorf("committed publication authority differs from planned target")
	}
	if head.AcceptedCandidateRef == nil || !evidenceRefsEqual(*head.AcceptedCandidateRef, *op.Publication.After) {
		return fmt.Errorf("committed publication candidate differs from planned target")
	}
	return nil
}

func (s *Store) verifyPublicationRemoteContainment(candidate AcceptedCandidate) error {
	if candidate.State == candidateObserved {
		remote, err := observePublicationRemote(s.identity.PrimaryRoot, candidate.Policy)
		if err != nil {
			return err
		}
		contains, err := isPublicationDescendant(s.identity.PrimaryRoot, candidate.CommitOID, remote)
		if err != nil {
			return err
		}
		if !contains {
			return fmt.Errorf("published immutable candidate disappeared from remote")
		}
	}
	return nil
}

func (s *Store) provePublicationEffects(op ExecutionOperation, c AcceptedCandidate) (map[string]string, error) {
	actual := make(map[string]string, len(op.Transition.Effects))
	for _, effect := range op.Transition.Effects {
		value, err := s.provePublicationEffect(op, c, effect)
		if err != nil {
			return nil, err
		}
		actual[effect.Key()] = value

	}
	return actual, nil
}

func (s *Store) provePublicationEffect(op ExecutionOperation, c AcceptedCandidate, effect EffectExpectation) (string, error) {
	switch effect.Surface {
	case MutationSurfaceRef:
		value, exists, err := readExecutionRef(s.identity.PrimaryRoot, effect.Resource)
		if err != nil {
			return "", err
		}
		if !exists || value != effect.ExpectedNew {
			return "", fmt.Errorf("publication local effect postcondition differs")
		}
		return value, nil
	case MutationSurfaceHistory:
		if c.State != candidateObserved {
			return "", fmt.Errorf("remote publication effect requires observed candidate")
		}
		return c.CommitOID, nil
	case MutationSurfaceState:
		if effect.Resource != "accepted-candidate" || effect.ExpectedNew != op.Publication.After.Digest {
			return "", fmt.Errorf("candidate state effect authority is inconsistent")
		}
		return op.Publication.After.Digest, nil
	case MutationSurfaceSource, MutationSurfaceIndex:
		return s.provePrimaryPromotionEffect(op, effect)
	default:
		return "", fmt.Errorf("unsupported publication effect surface")
	}
}

func (s *Store) provePrimaryPromotionEffect(op ExecutionOperation, effect EffectExpectation) (string, error) {
	if op.Transition.Kind != publicationPromote || effect.Resource != s.identity.PrimaryRoot {
		return "", fmt.Errorf("publication source/index effect is outside promotion authority")
	}
	if effect.Surface == MutationSurfaceSource {
		return captureParentSourceTree(s.identity.PrimaryRoot)
	}
	return gitTrimmed(s.identity.PrimaryRoot, "write-tree")
}
