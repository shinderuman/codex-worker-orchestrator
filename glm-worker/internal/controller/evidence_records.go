package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const evidenceJSONMediaType = "application/json"

func (s *Store) StoreAttemptSeal(record AttemptSeal) (EvidenceObjectRef, AttemptSeal, error) {
	if err := s.validateAttemptSeal(record); err != nil {
		return EvidenceObjectRef{}, AttemptSeal{}, err
	}
	record.EvidenceRefs = canonicalEvidenceRefs(record.EvidenceRefs)
	record.Missing = canonicalStrings(record.Missing)
	record.Unreadable = canonicalStrings(record.Unreadable)
	record.AttemptSealID = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, AttemptSeal{}, err
	}
	record.AttemptSealID = id
	ref, err := s.putEvidenceJSON("attempt-seal", id, record)
	return ref, record, err
}

func (s *Store) LoadAttemptSeal(ref EvidenceObjectRef) (AttemptSeal, error) {
	var record AttemptSeal
	if err := s.loadEvidenceJSON(ref, "attempt-seal", &record); err != nil {
		return AttemptSeal{}, err
	}
	id := record.AttemptSealID
	record.AttemptSealID = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return AttemptSeal{}, err
	}
	if id == "" || id != digest || id != ref.LogicalIdentity {
		return AttemptSeal{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "attempt seal identity is inconsistent"}
	}
	record.AttemptSealID = id
	if err := s.validateAttemptSeal(record); err != nil {
		return AttemptSeal{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func (s *Store) StoreAttemptFinalization(record AttemptFinalizationRecord) (EvidenceObjectRef, AttemptFinalizationRecord, error) {
	if err := s.validateFinalization(record); err != nil {
		return EvidenceObjectRef{}, AttemptFinalizationRecord{}, err
	}
	record.EvidenceRefs = canonicalEvidenceRefs(record.EvidenceRefs)
	record.FinalizationID = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, AttemptFinalizationRecord{}, err
	}
	record.FinalizationID = id
	ref, err := s.putEvidenceJSON("attempt-finalization", id, record)
	return ref, record, err
}

func (s *Store) StoreTaskIndexRevision(record TaskIndexRevision) (EvidenceObjectRef, TaskIndexRevision, error) {
	if err := validateTaskIndexRevision(record); err != nil {
		return EvidenceObjectRef{}, TaskIndexRevision{}, err
	}
	record.RevisionID = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, TaskIndexRevision{}, err
	}
	record.RevisionID = id
	ref, err := s.putEvidenceJSON("task-index-revision", taskEvidenceSubjectID(record.TaskRef), record)
	return ref, record, err
}

func (s *Store) StoreEpisodeIndexRevision(record EpisodeIndexRevision) (EvidenceObjectRef, EpisodeIndexRevision, error) {
	if err := validateEpisodeIndexRevision(record); err != nil {
		return EvidenceObjectRef{}, EpisodeIndexRevision{}, err
	}
	record.RevisionID = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, EpisodeIndexRevision{}, err
	}
	record.RevisionID = id
	ref, err := s.putEvidenceJSON("episode-index-revision", record.EpisodeID, record)
	return ref, record, err
}

func (s *Store) StoreEvidenceHead(record EvidenceHead) (EvidenceObjectRef, EvidenceHead, error) {
	if err := s.validateEvidenceHead(record); err != nil {
		return EvidenceObjectRef{}, EvidenceHead{}, err
	}
	record.TaskHeads = canonicalEvidenceHeads(record.TaskHeads)
	record.EpisodeHeads = canonicalEvidenceHeads(record.EpisodeHeads)
	record.HeadDigest = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, EvidenceHead{}, err
	}
	record.HeadDigest = id
	ref, err := s.putEvidenceJSON("evidence-head", s.identity.LineageID, record)
	return ref, record, err
}

func (s *Store) StoreEvidenceLedgerRecord(record EvidenceLedgerRecord) (EvidenceObjectRef, EvidenceLedgerRecord, error) {
	if err := s.validateEvidenceLedgerRecord(record); err != nil {
		return EvidenceObjectRef{}, EvidenceLedgerRecord{}, err
	}
	record.RecordDigest = ""
	id, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceObjectRef{}, EvidenceLedgerRecord{}, err
	}
	record.RecordDigest = id
	ref, err := s.putEvidenceJSON("evidence-ledger-record", fmt.Sprintf("%s:%d", s.identity.LineageID, record.Sequence), record)
	return ref, record, err
}

func (s *Store) validateAttemptSeal(record AttemptSeal) error {
	if record.SchemaVersion != evidenceSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.SemanticTaskRef.Empty() || record.RootTaskRef.Empty() {
		return fmt.Errorf("attempt seal repository/task identity is incomplete")
	}
	if strings.TrimSpace(record.AttemptID) == "" || record.ControllerGeneration == 0 || strings.TrimSpace(record.SealingTransitionID) == "" || strings.TrimSpace(record.RevokedLeaseID) == "" || strings.TrimSpace(record.WorkspaceID) == "" {
		return fmt.Errorf("attempt seal runtime identity is incomplete")
	}
	if strings.TrimSpace(record.ExecutionBaseOID) == "" || strings.TrimSpace(record.BaselineIndexTree) == "" || strings.TrimSpace(record.BaselineWorktreeTree) == "" || strings.TrimSpace(record.CurrentIndexTree) == "" || strings.TrimSpace(record.CurrentWorktreeTree) == "" {
		return fmt.Errorf("attempt seal tree identity is incomplete")
	}
	if strings.TrimSpace(record.ParentAuthorityDigest) == "" || strings.TrimSpace(record.ExecutionPurpose) == "" || strings.TrimSpace(record.Disposition) == "" || strings.TrimSpace(record.Coverage) == "" {
		return fmt.Errorf("attempt seal semantic identity is incomplete")
	}
	if err := validateTypedEvidenceRef(record.GitObjectArchive, "git-object-archive"); err != nil {
		return fmt.Errorf("attempt seal git archive reference: %w", err)
	}
	for _, ref := range record.EvidenceRefs {
		if err := validateEvidenceRef(ref); err != nil {
			return fmt.Errorf("attempt seal evidence reference: %w", err)
		}
	}
	return nil
}

func (s *Store) validateFinalization(record AttemptFinalizationRecord) error {
	if record.SchemaVersion != evidenceSchemaVersion || record.ControllerGeneration == 0 || strings.TrimSpace(record.TransitionID) == "" || strings.TrimSpace(record.ProjectSnapshotID) == "" || strings.TrimSpace(record.Kind) == "" {
		return fmt.Errorf("attempt finalization identity is incomplete")
	}
	if err := validateTypedEvidenceRef(record.AttemptSealRef, "attempt-seal"); err != nil {
		return fmt.Errorf("attempt finalization seal reference: %w", err)
	}
	if record.PreviousFinalization != nil {
		if err := validateTypedEvidenceRef(*record.PreviousFinalization, "attempt-finalization"); err != nil {
			return fmt.Errorf("attempt finalization previous reference: %w", err)
		}
	}
	return nil
}

func validateTaskIndexRevision(record TaskIndexRevision) error {
	if record.SchemaVersion != evidenceSchemaVersion || record.TaskRef.Empty() || record.ControllerGeneration == 0 {
		return fmt.Errorf("task evidence index identity is incomplete")
	}
	if record.PreviousRevision != nil {
		if err := validateTypedEvidenceRef(*record.PreviousRevision, "task-index-revision"); err != nil {
			return err
		}
	}
	return validateIndexEvidenceRefs(record.AttemptSeals, record.Finalizations)
}

func validateEpisodeIndexRevision(record EpisodeIndexRevision) error {
	if record.SchemaVersion != evidenceSchemaVersion || strings.TrimSpace(record.EpisodeID) == "" || record.EpisodeRevision == 0 || record.ControllerGeneration == 0 {
		return fmt.Errorf("episode evidence index identity is incomplete")
	}
	if record.PreviousRevision != nil {
		if err := validateTypedEvidenceRef(*record.PreviousRevision, "episode-index-revision"); err != nil {
			return err
		}
	}
	return validateIndexEvidenceRefs(record.AttemptSeals, record.Finalizations)
}

func validateIndexEvidenceRefs(seals, finalizations []EvidenceObjectRef) error {
	for _, ref := range seals {
		if err := validateTypedEvidenceRef(ref, "attempt-seal"); err != nil {
			return err
		}
	}
	for _, ref := range finalizations {
		if err := validateTypedEvidenceRef(ref, "attempt-finalization"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateEvidenceHead(record EvidenceHead) error {
	if record.SchemaVersion != evidenceSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.ControllerGeneration == 0 || strings.TrimSpace(record.ProjectSnapshotID) == "" {
		return fmt.Errorf("evidence head identity is incomplete")
	}
	if record.PreviousHead != nil {
		if err := validateTypedEvidenceRef(*record.PreviousHead, "evidence-head"); err != nil {
			return err
		}
	}
	for _, head := range append(append([]EvidenceSubjectHead(nil), record.TaskHeads...), record.EpisodeHeads...) {
		if strings.TrimSpace(head.SubjectID) == "" {
			return fmt.Errorf("evidence subject head identity is incomplete")
		}
		if err := validateEvidenceRef(head.RevisionRef); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateEvidenceLedgerRecord(record EvidenceLedgerRecord) error {
	if record.SchemaVersion != evidenceSchemaVersion || record.RepositoryIdentity != s.identity.LineageID || record.Sequence == 0 || record.ControllerGeneration == 0 || strings.TrimSpace(record.TransitionID) == "" || strings.TrimSpace(record.ProjectSnapshotID) == "" {
		return fmt.Errorf("evidence ledger identity is incomplete")
	}
	if err := validateTypedEvidenceRef(record.EvidenceHeadRef, "evidence-head"); err != nil {
		return err
	}
	if record.PreviousRecord != nil {
		if err := validateTypedEvidenceRef(*record.PreviousRecord, "evidence-ledger-record"); err != nil {
			return err
		}
	}
	return nil
}

func validateTypedEvidenceRef(ref EvidenceObjectRef, kind string) error {
	if err := validateEvidenceRef(ref); err != nil {
		return err
	}
	if ref.Kind != kind || ref.MediaType != evidenceJSONMediaType && kind != "git-object-archive" {
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence reference type is invalid"}
	}
	return nil
}

func (s *Store) putEvidenceJSON(kind, logicalIdentity string, value any) (EvidenceObjectRef, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	return s.PutEvidenceObject(kind, evidenceJSONMediaType, logicalIdentity, true, data)
}

func (s *Store) loadEvidenceJSON(ref EvidenceObjectRef, kind string, target any) error {
	if err := validateTypedEvidenceRef(ref, kind); err != nil {
		return err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return &EvidenceIntegrityError{Digest: ref.Digest, Reason: "record JSON is invalid"}
	}
	return nil
}

func evidenceRecordDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digestStrings("controller-evidence-record-v1", string(data)), nil
}

func taskEvidenceSubjectID(ref SemanticTaskRef) string {
	return digestStrings("controller-evidence-task-v1", ref.TaskPath, ref.ContractDigest)
}

func canonicalEvidenceRefs(refs []EvidenceObjectRef) []EvidenceObjectRef {
	result := append([]EvidenceObjectRef(nil), refs...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i].Kind + "\x00" + result[i].LogicalIdentity + "\x00" + result[i].Digest
		right := result[j].Kind + "\x00" + result[j].LogicalIdentity + "\x00" + result[j].Digest
		return left < right
	})
	return result
}

func canonicalEvidenceHeads(heads []EvidenceSubjectHead) []EvidenceSubjectHead {
	result := append([]EvidenceSubjectHead(nil), heads...)
	sort.Slice(result, func(i, j int) bool { return result[i].SubjectID < result[j].SubjectID })
	return result
}

func canonicalStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
