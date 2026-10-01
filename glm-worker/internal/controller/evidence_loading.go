package controller

import "fmt"

func (s *Store) LoadAttemptFinalization(ref EvidenceObjectRef) (AttemptFinalizationRecord, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"attempt-finalization",
		func(record *AttemptFinalizationRecord) (string, string) {
			id := record.FinalizationID
			record.FinalizationID = ""
			return id, id
		},
		func(record *AttemptFinalizationRecord, id string) { record.FinalizationID = id },
		validateFinalization,
		"attempt finalization identity is inconsistent",
	)
}

func (s *Store) LoadTaskIndexRevision(ref EvidenceObjectRef) (TaskIndexRevision, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"task-index-revision",
		func(record *TaskIndexRevision) (string, string) {
			id := record.RevisionID
			record.RevisionID = ""
			return id, taskEvidenceSubjectID(record.TaskRef)
		},
		func(record *TaskIndexRevision, id string) { record.RevisionID = id },
		validateTaskIndexRevision,
		"task index revision identity is inconsistent",
	)
}

func (s *Store) LoadEpisodeIndexRevision(ref EvidenceObjectRef) (EpisodeIndexRevision, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"episode-index-revision",
		func(record *EpisodeIndexRevision) (string, string) {
			id := record.RevisionID
			record.RevisionID = ""
			return id, record.EpisodeID
		},
		func(record *EpisodeIndexRevision, id string) { record.RevisionID = id },
		validateEpisodeIndexRevision,
		"episode index revision identity is inconsistent",
	)
}

func (s *Store) LoadEvidenceHead(ref EvidenceObjectRef) (EvidenceHead, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"evidence-head",
		func(record *EvidenceHead) (string, string) {
			id := record.HeadDigest
			record.HeadDigest = ""
			return id, record.RepositoryIdentity
		},
		func(record *EvidenceHead, id string) { record.HeadDigest = id },
		s.validateEvidenceHead,
		"evidence head identity is inconsistent",
	)
}

func (s *Store) LoadEvidenceLedgerRecord(ref EvidenceObjectRef) (EvidenceLedgerRecord, error) {
	return loadEvidenceRecord(
		s,
		ref,
		"evidence-ledger-record",
		func(record *EvidenceLedgerRecord) (string, string) {
			id := record.RecordDigest
			record.RecordDigest = ""
			return id, fmt.Sprintf("%s:%d", record.RepositoryIdentity, record.Sequence)
		},
		func(record *EvidenceLedgerRecord, id string) { record.RecordDigest = id },
		s.validateEvidenceLedgerRecord,
		"evidence ledger identity is inconsistent",
	)
}

func loadEvidenceRecord[T any](
	store *Store,
	ref EvidenceObjectRef,
	kind string,
	identity func(*T) (string, string),
	restore func(*T, string),
	validate func(T) error,
	reason string,
) (T, error) {
	var record T
	if err := store.loadEvidenceJSON(ref, kind, &record); err != nil {
		return record, err
	}
	id, logicalIdentity := identity(&record)
	digest, err := evidenceRecordDigest(record)
	if err != nil {
		return record, err
	}
	if id == "" || id != digest || logicalIdentity != ref.LogicalIdentity {
		return record, &EvidenceIntegrityError{Digest: ref.Digest, Reason: reason}
	}
	restore(&record, id)
	if err := validate(record); err != nil {
		return record, &EvidenceIntegrityError{Digest: ref.Digest, Reason: err.Error()}
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
