package controller

import "fmt"

func (s *Store) applyPublicationOperation(op ExecutionOperation) error {
	if op.Publication.After == nil {
		return s.applyBaseAdvancement(op)
	}
	candidate, err := s.LoadAcceptedCandidate(*op.Publication.After)
	if err != nil {
		return err
	}
	if err := s.guardPublicationRemote(op, candidate); err != nil {
		return err
	}
	if op.Transition.Kind == publicationPublish {
		return s.applyCandidatePublication(op, candidate)
	}
	if err := s.applyLocalPublicationEffects(op, candidate); err != nil {
		return err
	}
	observed := ""
	if candidate.State == candidateObserved {
		observed = op.Publication.RemoteOID
	}
	return s.commitPublicationOperation(op, candidate, observed)
}

func (s *Store) applyLocalPublicationEffects(op ExecutionOperation, candidate AcceptedCandidate) error {
	switch op.Transition.Kind {
	case publicationAccept:
		return s.applyAcceptedCandidateRetention(op, candidate)
	case publicationPromote:
		if err := updatePublicationRef(s.identity.PrimaryRoot, candidate.Policy.LocalRef, op.Publication.LocalOld, candidate.CommitOID); err != nil {
			return err
		}
	case publicationRebind:
		if err := s.retainAcceptedCandidate(candidate); err != nil {
			return err
		}
		return s.applyPlannedLocalRef(op, candidate)
	case publicationReenter:
		return s.applyPlannedLocalRef(op, candidate)
	case publicationAdopt:
		return s.applyAdvancementLocalRef(op)
	case publicationRevalidate:
	default:
		return fmt.Errorf("unsupported publication operation")
	}
	return nil
}

func (s *Store) retainAcceptedCandidate(candidate AcceptedCandidate) error {
	ref := candidateRetentionRef(candidate.CommitOID)
	actual, exists, err := readExecutionRef(s.identity.PrimaryRoot, ref)
	if err != nil {
		return err
	}
	if exists {
		if actual != candidate.CommitOID {
			return fmt.Errorf("candidate retention ref was replaced")
		}
		return nil
	}
	_, err = runGitBinary(s.identity.PrimaryRoot, nil, "update-ref", ref, candidate.CommitOID, "")
	return err
}

func (s *Store) commitPublicationOperation(op ExecutionOperation, c AcceptedCandidate, observedRemote string) error {
	observation, err := s.storePublicationObservation(op, c, observedRemote)
	if err != nil {
		return err
	}
	evidence, err := s.publicationEvidence(op, c, *op.Publication.After, observation)
	if err != nil {
		return err
	}
	actual, err := s.provePublicationEffects(op, c)
	if err != nil {
		return err
	}
	_, _, err = s.commitAuthorityTransitionWithEvidenceLocked(op.Transition, actual, evidence, func(next *RepositoryControllerHead) error {
		next.AcceptedCandidateRef = op.Publication.After
		next.PublicationPolicy = &op.Publication.Policy
		next.LiveAttemptID = ""
		next.LiveLeaseID = ""
		next.IntegrationTip = op.Publication.NewTip
		if c.State == candidateObserved {
			next.ObservedPrefix = c.CommitOID
			next.ObservedRemoteTip = observedRemote
		}
		return s.commitPublicationTarget(next, op, c)
	})
	return err
}

func (s *Store) commitPublicationTarget(next *RepositoryControllerHead, op ExecutionOperation, c AcceptedCandidate) error {
	if op.Transition.Kind == publicationAccept {
		attempt := op.Source.Attempt
		attempt.AttemptState = AttemptStateAccepted
		attempt.AttemptSealID = c.SealRef.LogicalIdentity
		if err := s.writeAttempt(attempt); err != nil {
			return err
		}
	}
	if op.Publication.Project != nil {
		if err := s.writeProjectSnapshot(*op.Publication.Project); err != nil {
			return err
		}
		next.ProjectSnapshotID = op.Publication.Project.SnapshotID
	}
	if op.Publication.Episode != nil {
		if err := s.writeEpisodeRevision(*op.Publication.Episode); err != nil {
			return err
		}
		next.ActiveEpisodeRevision = op.Publication.Episode.Revision
	}
	return nil
}

func (s *Store) applyPlannedLocalRef(op ExecutionOperation, c AcceptedCandidate) error {
	if op.Publication.LocalOld == "" {
		return nil
	}
	return updatePublicationRef(s.identity.PrimaryRoot, c.Policy.LocalRef, op.Publication.LocalOld, op.Publication.LocalNew)
}

func (s *Store) applyAcceptedCandidateRetention(op ExecutionOperation, c AcceptedCandidate) error {
	if err := s.verifyAcceptanceSource(op); err != nil {
		return err
	}
	if err := s.retainSuspension(op.Source.Workspace.Root, *op.Suspension); err != nil {
		return err
	}
	return s.retainAcceptedCandidate(c)
}
