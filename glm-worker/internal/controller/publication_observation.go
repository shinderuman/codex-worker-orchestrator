package controller

import "fmt"

func (s *Store) storePublicationObservation(op ExecutionOperation, c AcceptedCandidate, remote string) (*EvidenceObjectRef, error) {
	if remote == "" {
		return nil, nil
	}
	contains, err := isPublicationDescendant(s.identity.PrimaryRoot, c.CommitOID, remote)
	if err != nil {
		return nil, err
	}
	if !contains {
		return nil, fmt.Errorf("publication observation does not contain candidate")
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "remote:"+remote, []string{remote})
	if err != nil {
		return nil, err
	}
	record := PublicationObservation{SchemaVersion: controllerSchemaVersion, TransitionID: op.Transition.TransitionID, CandidateID: c.CandidateID, Remote: c.Policy.Remote, RemoteRef: c.Policy.RemoteRef, RemoteOID: remote, CandidateOID: c.CommitOID, GitArchive: archive}
	ref, err := s.putEvidenceJSON("publication-observation", op.Transition.TransitionID, record)
	return &ref, err
}
