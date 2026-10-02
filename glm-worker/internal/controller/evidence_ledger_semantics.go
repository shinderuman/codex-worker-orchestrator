package controller

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (record EvidenceLedgerRecord) MarshalJSON() ([]byte, error) {
	canonical, err := canonicalEvidenceLedgerRecord(record)
	if err != nil {
		return nil, err
	}
	type evidenceLedgerRecordJSON EvidenceLedgerRecord
	return json.Marshal(evidenceLedgerRecordJSON(canonical))
}

func (record *EvidenceLedgerRecord) UnmarshalJSON(data []byte) error {
	type evidenceLedgerRecordJSON EvidenceLedgerRecord
	var decoded evidenceLedgerRecordJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	canonical, err := canonicalEvidenceLedgerRecord(EvidenceLedgerRecord(decoded))
	if err != nil {
		return err
	}
	*record = canonical
	return nil
}

func canonicalEvidenceLedgerRecord(record EvidenceLedgerRecord) (EvidenceLedgerRecord, error) {
	if err := validateEvidenceLedgerDelta(record); err != nil {
		return EvidenceLedgerRecord{}, err
	}
	record.ChangedTaskIndexHeads = canonicalEvidenceHeads(record.ChangedTaskIndexHeads)
	record.ChangedEpisodeIndexHeads = canonicalEvidenceHeads(record.ChangedEpisodeIndexHeads)
	record.AttemptSealsAdded = canonicalEvidenceRefs(record.AttemptSealsAdded)
	record.FinalizationRecordsAdded = canonicalEvidenceRefs(record.FinalizationRecordsAdded)
	record.FindingRecordsAdded = canonicalEvidenceRefs(record.FindingRecordsAdded)
	return record, nil
}

func validateEvidenceLedgerDelta(record EvidenceLedgerRecord) error {
	if err := validateLedgerChangedHeads(record.ChangedTaskIndexHeads, "task-index-revision"); err != nil {
		return fmt.Errorf("changed task index heads: %w", err)
	}
	if err := validateLedgerChangedHeads(record.ChangedEpisodeIndexHeads, "episode-index-revision"); err != nil {
		return fmt.Errorf("changed episode index heads: %w", err)
	}
	if err := validateUniqueTypedEvidenceRefs(record.AttemptSealsAdded, "attempt-seal"); err != nil {
		return fmt.Errorf("attempt seals added: %w", err)
	}
	if err := validateUniqueTypedEvidenceRefs(record.FinalizationRecordsAdded, "attempt-finalization"); err != nil {
		return fmt.Errorf("finalization records added: %w", err)
	}
	if err := validateUniqueTypedEvidenceRefs(record.FindingRecordsAdded, "finding-record"); err != nil {
		return fmt.Errorf("finding records added: %w", err)
	}
	return nil
}

func validateLedgerChangedHeads(heads []EvidenceSubjectHead, kind string) error {
	seen := map[string]bool{}
	for _, head := range heads {
		if strings.TrimSpace(head.SubjectID) == "" {
			return fmt.Errorf("subject identity is incomplete")
		}
		if seen[head.SubjectID] {
			return fmt.Errorf("duplicate subject %s", head.SubjectID)
		}
		seen[head.SubjectID] = true
		if err := validateTypedEvidenceRef(head.RevisionRef, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateUniqueTypedEvidenceRefs(refs []EvidenceObjectRef, kind string) error {
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := validateTypedEvidenceRef(ref, kind); err != nil {
			return err
		}
		key := evidenceRefKey(ref)
		if seen[key] {
			return fmt.Errorf("duplicate %s evidence reference", kind)
		}
		seen[key] = true
	}
	return nil
}
