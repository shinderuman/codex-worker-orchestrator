package controller

import (
	"fmt"
)

type publicationSourceContinuity int

const (
	publicationSourceContinuous publicationSourceContinuity = iota
	publicationSourceAdvanced
	publicationSourceConflict
)

func (s *Store) candidateSourceContinuity(c AcceptedCandidate) (publicationSourceContinuity, string, error) {
	local, exists, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil {
		return publicationSourceConflict, "", err
	}
	if !exists {
		return publicationSourceConflict, "", fmt.Errorf("publication local ref is missing")
	}
	if local == c.BaseOID || local == c.CommitOID {
		return publicationSourceContinuous, local, nil
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, c.BaseOID, local)
	if err != nil {
		return publicationSourceConflict, "", err
	}
	if !descendant {
		return publicationSourceConflict, local, nil
	}
	return publicationSourceAdvanced, local, nil
}

func (s *Store) requireCurrentCandidateSource(c AcceptedCandidate) error {
	if c.State == candidateObserved {
		return nil
	}
	continuity, tip, err := s.candidateSourceContinuity(c)
	if err != nil {
		return err
	}
	switch continuity {
	case publicationSourceContinuous:
		return nil
	case publicationSourceAdvanced:
		return fmt.Errorf("candidate source is stale: primary advanced to %s beyond the candidate base %s; run the projected controller-publication rebind before revalidation, final validation, installation, or promotion", tip, c.BaseOID)
	default:
		return fmt.Errorf("publication local ref %s is outside candidate promotion authority and is not a fast-forward of the candidate base; resolve the primary divergence before publication", tip)
	}
}

func (s *Store) admissionSourceContinuity(head RepositoryControllerHead) (publicationSourceContinuity, string, error) {
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return publicationSourceConflict, "", fmt.Errorf("committed primary authority is unreadable: %w", err)
	}
	if authority.Snapshot.HeadOID == head.IntegrationTip {
		return publicationSourceContinuous, authority.Snapshot.HeadOID, nil
	}
	descendant, err := isPublicationDescendant(s.identity.PrimaryRoot, head.IntegrationTip, authority.Snapshot.HeadOID)
	if err != nil {
		return publicationSourceConflict, "", err
	}
	if !descendant {
		return publicationSourceConflict, authority.Snapshot.HeadOID, nil
	}
	return publicationSourceAdvanced, authority.Snapshot.HeadOID, nil
}

func (s *Store) requireCurrentAdmissionSource(head RepositoryControllerHead) error {
	continuity, tip, err := s.admissionSourceContinuity(head)
	if err != nil {
		return err
	}
	switch continuity {
	case publicationSourceContinuous:
		return nil
	case publicationSourceAdvanced:
		return fmt.Errorf("primary advanced to %s beyond the execution source %s; adopt the external advancement before accepting a candidate on the stale source", tip, head.IntegrationTip)
	default:
		return fmt.Errorf("primary HEAD %s is not a fast-forward of the controller integration authority %s; resolve the primary divergence before accepting a candidate", tip, head.IntegrationTip)
	}
}
