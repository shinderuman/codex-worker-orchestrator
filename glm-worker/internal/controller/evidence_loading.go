package controller

import "fmt"

func (s *Store) LoadAttemptFinalization(ref EvidenceObjectRef) (AttemptFinalizationRecord, error) {
	var record AttemptFinalizationRecord
	if err := s.loadEvidenceJSON(ref, "attempt-finalization", &record); err != nil {
		return AttemptFinalizationRecord{}, err
	}
	id := record.FinalizationID
	record.FinalizationID = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return AttemptFinalizationRecord{}, err
	}
	if id == "" || id != digest || id != ref.LogicalIdentity {
		return AttemptFinalizationRecord{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "attempt finalization identity is inconsistent"}
	}
	record.FinalizationID = id
	if err := s.validateFinalization(record); err != nil {
		return AttemptFinalizationRecord{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func (s *Store) LoadTaskIndexRevision(ref EvidenceObjectRef) (TaskIndexRevision, error) {
	var record TaskIndexRevision
	if err := s.loadEvidenceJSON(ref, "task-index-revision", &record); err != nil {
		return TaskIndexRevision{}, err
	}
	id := record.RevisionID
	record.RevisionID = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return TaskIndexRevision{}, err
	}
	if id == "" || id != digest || taskEvidenceSubjectID(record.TaskRef) != ref.LogicalIdentity {
		return TaskIndexRevision{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "task index revision identity is inconsistent"}
	}
	record.RevisionID = id
	if err := validateTaskIndexRevision(record); err != nil {
		return TaskIndexRevision{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func (s *Store) LoadEpisodeIndexRevision(ref EvidenceObjectRef) (EpisodeIndexRevision, error) {
	var record EpisodeIndexRevision
	if err := s.loadEvidenceJSON(ref, "episode-index-revision", &record); err != nil {
		return EpisodeIndexRevision{}, err
	}
	id := record.RevisionID
	record.RevisionID = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return EpisodeIndexRevision{}, err
	}
	if id == "" || id != digest || record.EpisodeID != ref.LogicalIdentity {
		return EpisodeIndexRevision{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "episode index revision identity is inconsistent"}
	}
	record.RevisionID = id
	if err := validateEpisodeIndexRevision(record); err != nil {
		return EpisodeIndexRevision{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func (s *Store) LoadEvidenceHead(ref EvidenceObjectRef) (EvidenceHead, error) {
	var record EvidenceHead
	if err := s.loadEvidenceJSON(ref, "evidence-head", &record); err != nil {
		return EvidenceHead{}, err
	}
	id := record.HeadDigest
	record.HeadDigest = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceHead{}, err
	}
	if id == "" || id != digest || record.RepositoryIdentity != ref.LogicalIdentity {
		return EvidenceHead{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence head identity is inconsistent"}
	}
	record.HeadDigest = id
	if err := s.validateEvidenceHead(record); err != nil {
		return EvidenceHead{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func (s *Store) LoadEvidenceLedgerRecord(ref EvidenceObjectRef) (EvidenceLedgerRecord, error) {
	var record EvidenceLedgerRecord
	if err := s.loadEvidenceJSON(ref, "evidence-ledger-record", &record); err != nil {
		return EvidenceLedgerRecord{}, err
	}
	id := record.RecordDigest
	record.RecordDigest = ""
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return EvidenceLedgerRecord{}, err
	}
	logicalIdentity := fmt.Sprintf("%s:%d", record.RepositoryIdentity, record.Sequence)
	if id == "" || id != digest || logicalIdentity != ref.LogicalIdentity {
		return EvidenceLedgerRecord{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: "evidence ledger identity is inconsistent"}
	}
	record.RecordDigest = id
	if err := s.validateEvidenceLedgerRecord(record); err != nil {
		return EvidenceLedgerRecord{}, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
	}
	return record, nil
}

func evidenceRefsEqual(left, right EvidenceObjectRef) bool {
	return left.Digest == right.Digest &&
		left.Kind == right.Kind &&
		left.MediaType == right.MediaType &&
		left.Length == right.Length &&
		left.LogicalIdentity == right.LogicalIdentity &&
		left.Required == right.Required
}
