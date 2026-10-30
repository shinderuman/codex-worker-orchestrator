package controller

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	candidateEvidencePass             = "pass"
	candidateEvidenceAccepted         = "accepted"
	candidateEvidenceKindReview       = "review"
	candidateEvidenceKindValidation   = "validation"
	candidateEvidenceKindInstall      = "install"
	candidateEvidenceKindParentAccept = "parent-acceptance"

	evidenceKindCandidateEvidence = "candidate-evidence"
)

func validateCandidateEvidenceMetadata(record CandidateEvidence, repository string) error {
	if !candidateEvidenceIdentityComplete(record, repository) {
		return fmt.Errorf("candidate evidence identity/result is incomplete")
	}
	if record.Kind == candidateEvidenceKindValidationObligation || record.Kind == candidateEvidenceKindInstallObligation {
		if record.Result != candidateEvidenceDeclared {
			return fmt.Errorf("an obligation must be declared, never reported as successful execution")
		}
		return nil
	}
	if record.Kind == candidateEvidenceKindParentAccept {
		if record.Result != candidateEvidenceAccepted {
			return fmt.Errorf("parent acceptance evidence must record the acceptance decision, not an execution result")
		}
		return nil
	}
	if record.Result != candidateEvidencePass {
		return fmt.Errorf("candidate evidence is not a passing result")
	}
	if record.Kind != candidateEvidenceKindReview && record.Kind != candidateEvidenceKindValidation && record.Kind != candidateEvidenceKindInstall {
		return fmt.Errorf("unsupported candidate evidence kind")
	}
	return nil
}

func candidateEvidenceIdentityComplete(record CandidateEvidence, repository string) bool {
	return record.SchemaVersion == controllerSchemaVersion && record.RepositoryIdentity == repository && record.AttemptID != "" && record.SnapshotID != "" && record.BaseOID != "" && record.TreeOID != ""
}

func (s *Store) StoreCandidateEvidenceJSON(record CandidateEvidence, artifact json.RawMessage) (EvidenceObjectRef, error) {
	if err := validateCandidateEvidenceMetadata(record, s.identity.LineageID); err != nil {
		return EvidenceObjectRef{}, err
	}
	if record.Artifact != (EvidenceObjectRef{}) {
		return EvidenceObjectRef{}, fmt.Errorf("candidate evidence requires either artifact reference or JSON, not both")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(artifact, &object); err != nil || object == nil {
		return EvidenceObjectRef{}, fmt.Errorf("candidate evidence artifact must be a JSON object")
	}
	ref, err := s.PutEvidenceObject(record.Kind+"-artifact", "application/json", record.AttemptID+":"+record.Kind+":"+record.SnapshotID, true, artifact)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	record.Artifact = ref
	return s.StoreCandidateEvidence(record)
}

func (s *Store) StoreCandidateEvidence(record CandidateEvidence) (EvidenceObjectRef, error) {
	if err := validateCandidateEvidenceMetadata(record, s.identity.LineageID); err != nil {
		return EvidenceObjectRef{}, err
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
	return s.loadAcceptedCandidateRecord(ref, s.validateAcceptedCandidate)
}

func (s *Store) loadCandidateArchive(ref EvidenceObjectRef) (AcceptedCandidate, error) {
	return s.loadAcceptedCandidateRecord(ref, s.validateCandidateArchive)
}

func (s *Store) loadAcceptedCandidateRecord(ref EvidenceObjectRef, validate func(AcceptedCandidate) error) (AcceptedCandidate, error) {
	return loadEvidenceRecord(s, ref, "accepted-candidate", func(c *AcceptedCandidate) (string, string) {
		id := c.RevisionID
		c.RevisionID = ""
		return id, c.CandidateID
	}, func(c *AcceptedCandidate, id string) { c.RevisionID = id }, validate, "accepted candidate identity is inconsistent")
}

func (s *Store) validateAcceptedCandidate(candidate AcceptedCandidate) error {
	if err := s.validateCandidateArchive(candidate); err != nil {
		return err
	}
	if err := validatePublicationPolicy(s.identity.PrimaryRoot, candidate.Policy); err != nil {
		return err
	}
	return s.validateCandidateProofs(candidate)
}

func (s *Store) validateCandidateArchive(candidate AcceptedCandidate) error {
	if err := validateCandidateIdentity(candidate); err != nil {
		return err
	}
	if candidate.State != candidatePrepared && candidate.State != candidatePromoted && candidate.State != candidateObserved {
		return fmt.Errorf("accepted candidate state is invalid")
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
	return nil
}

func (s *Store) validateCandidateProofs(candidate AcceptedCandidate) error {
	implementation, err := s.completionImplementation(candidate)
	if err != nil {
		return err
	}
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
		return s.validateCandidateEvidence(implementation, candidate.Evidence)
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
	if seen[candidateEvidenceKindParentAccept] && seen[candidateEvidenceKindReview] {
		return fmt.Errorf("candidate carries both parent acceptance and independent review authority")
	}
	if !candidateEvidenceComplete(candidate, seen) {
		return fmt.Errorf("candidate review/validation/install evidence requires re-entry")
	}
	return nil
}

func candidateEvidenceComplete(candidate AcceptedCandidate, seen map[string]bool) bool {
	installPending := candidate.State == candidatePrepared && seen[candidateEvidenceKindInstallObligation]
	accepted := seen[candidateEvidenceKindReview] || seen[candidateEvidenceKindParentAccept]
	validated := seen[candidateEvidenceKindValidation] || seen[candidateEvidenceKindValidationObligation]
	installed := seen[candidateEvidenceKindInstall] || installPending || !candidate.Policy.RequireInstall
	return accepted && validated && installed
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
	if validate := s.obligationValidation(record.Kind); validate != nil {
		return record.Kind, validate(candidate, record.Artifact)
	}
	if record.Kind == candidateEvidenceKindParentAccept {
		return record.Kind, s.validateParentAcceptanceArtifact(candidate, record.Artifact)
	}
	if record.Kind != candidateEvidenceKindReview && record.Kind != candidateEvidenceKindValidation && record.Kind != candidateEvidenceKindInstall {
		return "", fmt.Errorf("invalid candidate evidence kind")
	}
	return record.Kind, nil
}

func candidateEvidenceMatches(record CandidateEvidence, candidate AcceptedCandidate, repository string) bool {
	return validateCandidateEvidenceMetadata(record, repository) == nil && record.AttemptID == candidate.AttemptID && record.SnapshotID == candidate.SnapshotID && record.BaseOID == candidate.BaseOID && record.TreeOID == candidate.TreeOID
}

func (s *Store) obligationValidation(kind string) func(AcceptedCandidate, EvidenceObjectRef) error {
	switch kind {
	case candidateEvidenceKindValidationObligation:
		return s.validateCandidateValidationObligation
	case candidateEvidenceKindInstallObligation:
		return s.validateCandidateInstallObligation
	default:
		return nil
	}
}
