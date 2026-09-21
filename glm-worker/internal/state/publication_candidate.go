package state

import (
	"encoding/json"
	"fmt"
	"time"
)

type PublicationCandidate struct {
	Version       int            `json:"version"`
	TaskID        string         `json:"task_id"`
	BaseHead      string         `json:"base_head"`
	CommitOID     string         `json:"commit_oid"`
	TreeOID       string         `json:"tree_oid"`
	MessageDigest string         `json:"message_sha256"`
	Snapshot      SnapshotDigest `json:"snapshot"`
	SnapshotID    string         `json:"snapshot_id"`
	PreparedAt    time.Time      `json:"prepared_at"`
}

const (
	publicationCandidateStateFile = "publication-candidate.json"
	publicationCandidateVersion   = 1
)

func (s *StateStore) SavePublicationCandidate(candidate PublicationCandidate) error {
	if err := validatePublicationCandidate(candidate); err != nil {
		return err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if candidate.TaskID != taskID {
		return fmt.Errorf("publication candidate task %s does not match current task %s", candidate.TaskID, taskID)
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return fmt.Errorf("publication candidateをJSON化できません: %w", err)
	}
	return s.Write(publicationCandidateStateFile, string(data))
}

func (s *StateStore) LoadPublicationCandidate() (PublicationCandidate, error) {
	data, err := s.Read(publicationCandidateStateFile)
	if err != nil {
		return PublicationCandidate{}, err
	}
	var candidate PublicationCandidate
	if err := json.Unmarshal([]byte(data), &candidate); err != nil {
		return PublicationCandidate{}, fmt.Errorf("publication candidateを読めません: %w", err)
	}
	if err := validatePublicationCandidate(candidate); err != nil {
		return PublicationCandidate{}, err
	}
	return candidate, nil
}

func (s *StateStore) ClearPublicationCandidate() error {
	return s.Remove(publicationCandidateStateFile)
}

func validatePublicationCandidate(candidate PublicationCandidate) error {
	if err := validatePublicationCandidateIdentity(candidate); err != nil {
		return err
	}
	if err := validatePublicationCandidateSnapshot(candidate); err != nil {
		return err
	}
	if candidate.PreparedAt.IsZero() {
		return fmt.Errorf("publication candidate prepared_atがありません")
	}
	return nil
}

func validatePublicationCandidateIdentity(candidate PublicationCandidate) error {
	if candidate.Version != publicationCandidateVersion {
		return fmt.Errorf("unsupported publication candidate version: %d", candidate.Version)
	}
	if !ValidGeneratedUUID(candidate.TaskID) {
		return fmt.Errorf("publication candidate task IDが不正です")
	}
	if !validPublicationOID(candidate.BaseHead) || !validPublicationOID(candidate.CommitOID) || !validPublicationOID(candidate.TreeOID) {
		return fmt.Errorf("publication candidate git identityが不正です")
	}
	if !validPublicationDigest(candidate.MessageDigest) {
		return fmt.Errorf("publication candidate commit message digestが不正です")
	}
	return nil
}

func validatePublicationCandidateSnapshot(candidate PublicationCandidate) error {
	if candidate.Snapshot.Head != candidate.BaseHead || candidate.Snapshot.IndexDigest == "" || candidate.Snapshot.WorktreeDigest == "" {
		return fmt.Errorf("publication candidate snapshot identityが不正です")
	}
	expectedSnapshotID := ValidationSnapshotID(candidate.Snapshot.Head, candidate.Snapshot.IndexDigest, candidate.Snapshot.WorktreeDigest)
	if expectedSnapshotID == "" || candidate.SnapshotID != expectedSnapshotID {
		return fmt.Errorf("publication candidate snapshot IDが一致しません")
	}
	return nil
}

func PublicationValidationSnapshotIDs(candidate PublicationCandidate, current SnapshotDigest) []string {
	ids := []string{candidate.SnapshotID}
	if current.Head != candidate.CommitOID || current.IndexDigest == "" || current.WorktreeDigest == "" {
		return ids
	}
	if promoted := ValidationSnapshotID(current.Head, current.IndexDigest, current.WorktreeDigest); promoted != "" {
		ids = append(ids, promoted)
	}
	return ids
}

func validPublicationOID(value string) bool {
	return validPublicationHex(value, 40)
}

func validPublicationDigest(value string) bool {
	return validPublicationHex(value, 64)
}

func validPublicationHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
