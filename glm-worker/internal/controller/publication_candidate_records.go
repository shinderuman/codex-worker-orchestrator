package controller

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	candidateEvidenceKindReview     = "review"
	candidateEvidenceKindValidation = "validation"
	candidateEvidenceKindInstall    = "install"

	evidenceKindCandidateEvidence = "candidate-evidence"
)

func (s *Store) StoreCandidateEvidence(record CandidateEvidence) (EvidenceObjectRef, error) {
	if record.SchemaVersion != controllerSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.AttemptID == "" || record.SnapshotID == "" || record.BaseOID == "" || record.TreeOID == "" || record.Result != "pass" {
		return EvidenceObjectRef{}, fmt.Errorf("candidate evidence identity/result is incomplete")
	}
	if record.Kind != candidateEvidenceKindReview && record.Kind != candidateEvidenceKindValidation && record.Kind != candidateEvidenceKindInstall {
		return EvidenceObjectRef{}, fmt.Errorf("unsupported candidate evidence kind")
	}
	if _, err := s.LoadEvidenceObject(record.Artifact); err != nil {
		return EvidenceObjectRef{}, err
	}
	return s.putEvidenceJSON(evidenceKindCandidateEvidence, record.AttemptID+":"+record.Kind, record)
}

func (s *Store) storeAcceptedCandidate(candidate AcceptedCandidate) (EvidenceObjectRef, error) {
	candidate.CandidateID = acceptedCandidateID(candidate)
	candidate.RevisionID = ""
	id, err := evidenceRecordDigest(candidate)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	candidate.RevisionID = id
	return s.putEvidenceJSON("accepted-candidate", candidate.CandidateID, candidate)
}

func (s *Store) LoadAcceptedCandidate(ref EvidenceObjectRef) (AcceptedCandidate, error) {
	return loadEvidenceRecord(s, ref, "accepted-candidate", func(c *AcceptedCandidate) (string, string) {
		id := c.RevisionID
		c.RevisionID = ""
		return id, c.CandidateID
	}, func(c *AcceptedCandidate, id string) { c.RevisionID = id }, s.validateAcceptedCandidate, "accepted candidate identity is inconsistent")
}

func (s *Store) validateAcceptedCandidate(candidate AcceptedCandidate) error {
	if err := validateCandidateIdentity(candidate); err != nil {
		return err
	}
	if candidate.State != candidatePrepared && candidate.State != candidatePromoted && candidate.State != candidateObserved {
		return fmt.Errorf("accepted candidate state is invalid")
	}
	if err := validatePublicationPolicy(s.identity.PrimaryRoot, candidate.Policy); err != nil {
		return err
	}
	if _, err := s.LoadAttemptSeal(candidate.SealRef); err != nil {
		return err
	}
	if candidate.CandidateID != acceptedCandidateID(candidate) {
		return fmt.Errorf("candidate content identity is inconsistent")
	}
	if _, err := s.VerifyGitObjectArchive(candidate.GitArchive); err != nil {
		return err
	}
	return s.validateCandidateProofs(candidate)
}

func (s *Store) validateCandidateProofs(candidate AcceptedCandidate) error {
	if len(candidate.DescendantEvidence) != 0 {
		tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", candidate.DescendantTip+"^{tree}")
		if err != nil {
			return err
		}
		if err := s.validateCandidateEvidence(canonicalDescendantEvidenceView(candidate, tree), candidate.DescendantEvidence); err != nil {
			return err
		}
	}
	if candidate.EvidenceValid {
		return s.validateCandidateEvidence(candidate, candidate.Evidence)
	}
	return nil
}

func (s *Store) validateCandidateEvidence(candidate AcceptedCandidate, refs []EvidenceObjectRef) error {
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := validateTypedEvidenceRef(ref, evidenceKindCandidateEvidence); err != nil {
			return err
		}
		kind, err := s.validateCandidateEvidenceRecord(candidate, ref)
		if err != nil {
			return err
		}
		seen[kind] = true
	}
	if !seen[candidateEvidenceKindReview] || !seen[candidateEvidenceKindValidation] || (candidate.Policy.RequireInstall && !seen[candidateEvidenceKindInstall]) {
		return fmt.Errorf("candidate review/validation/install evidence requires re-entry")
	}
	return nil
}

func acceptedCandidateID(c AcceptedCandidate) string {
	return digestStrings("accepted-candidate-v1", c.AttemptID, c.TaskRef.TaskPath, c.TaskRef.ContractDigest, c.BaseOID, c.CommitOID, c.TreeOID, c.SnapshotID, c.Message, c.Policy.Remote, c.Policy.RemoteRef, c.Policy.LocalRef, strconv.FormatBool(c.Policy.RequireInstall))
}

func validateCandidateIdentity(c AcceptedCandidate) error {
	if c.SchemaVersion != controllerSchemaVersion || c.TaskRef.Empty() || c.RootTaskRef.Empty() || c.ControllerGeneration == 0 {
		return fmt.Errorf("candidate typed identity is incomplete")
	}
	for _, value := range []string{c.AttemptID, c.ProjectSnapshotID, c.TransitionID, c.BaseOID, c.TreeOID, c.CommitOID, c.SnapshotID, c.Message} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("candidate content identity is incomplete")
		}
	}
	return nil
}

func (s *Store) validateCandidateEvidenceRecord(candidate AcceptedCandidate, ref EvidenceObjectRef) (string, error) {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return "", err
	}
	var record CandidateEvidence
	if err := json.Unmarshal(data, &record); err != nil {
		return "", err
	}
	if !candidateEvidenceMatches(record, candidate, s.identity.LineageID) {
		return "", fmt.Errorf("candidate evidence is stale or unsuccessful")
	}
	if record.CandidateID != "" && record.CandidateID != candidate.CandidateID {
		return "", fmt.Errorf("candidate evidence binds a different candidate")
	}
	if _, err := s.LoadEvidenceObject(record.Artifact); err != nil {
		return "", err
	}
	if record.Kind != candidateEvidenceKindReview && record.Kind != candidateEvidenceKindValidation && record.Kind != candidateEvidenceKindInstall {
		return "", fmt.Errorf("invalid candidate evidence kind")
	}
	return record.Kind, nil
}

func candidateEvidenceMatches(record CandidateEvidence, candidate AcceptedCandidate, repository string) bool {
	return record.SchemaVersion == controllerSchemaVersion && record.RepositoryIdentity == repository && record.AttemptID == candidate.AttemptID && record.SnapshotID == candidate.SnapshotID && record.BaseOID == candidate.BaseOID && record.TreeOID == candidate.TreeOID && record.Result == "pass"
}
