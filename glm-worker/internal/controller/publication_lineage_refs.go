package controller

import "encoding/json"

func (s *Store) candidatePublicationRefs(c AcceptedCandidate, lineage EvidenceObjectRef, observation *EvidenceObjectRef) ([]EvidenceObjectRef, error) {
	refs := []EvidenceObjectRef{lineage, c.GitArchive}
	refs = append(refs, c.Evidence...)
	refs = append(refs, c.DescendantEvidence...)
	for _, ref := range append(c.Evidence, c.DescendantEvidence...) {
		data, err := s.LoadEvidenceObject(ref)
		if err != nil {
			return nil, err
		}
		var evidence CandidateEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			return nil, err
		}
		refs = append(refs, evidence.Artifact)
	}
	if observation == nil {
		return canonicalEvidenceRefs(refs), nil
	}
	data, err := s.LoadEvidenceObject(*observation)
	if err != nil {
		return nil, err
	}
	var record PublicationObservation
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	refs = append(refs, *observation, record.GitArchive)
	return canonicalEvidenceRefs(refs), nil
}
