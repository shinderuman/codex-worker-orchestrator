package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type PublicationInvalidatingFinding struct {
	Version             int    `json:"version"`
	TaskID              string `json:"task_id"`
	FindingID           string `json:"finding_id"`
	Disposition         string `json:"disposition"`
	CandidateCommitOID  string `json:"candidate_commit_oid"`
	CandidateSnapshotID string `json:"candidate_snapshot_id"`
	Origin              string `json:"origin,omitempty"`
	Cause               string `json:"cause,omitempty"`
}

const (
	publicationInvalidatingFindingStateFile = "publication-invalidating-finding.json"
	publicationInvalidatingFindingVersion   = 1
	PublicationFindingCorrectnessDefect     = "correctness-defect"
)

func (s *StateStore) RecordPublicationInvalidatingFinding(findingID, candidateOID, snapshotID, origin, cause string) error {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return fmt.Errorf("publication invalidating finding requires %s, got %s", TaskStatusAwaitingParentCompletion, s.TaskStatus())
	}
	if !ValidGeneratedUUID(findingID) {
		return fmt.Errorf("publication invalidating finding ID is invalid")
	}
	if err := validateParentFixDeclaration(origin, cause); err != nil {
		return err
	}
	completion, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return err
	}
	if completion == nil || completion.Terminal != SessionRotationTerminalAccept {
		return fmt.Errorf("publication invalidating finding requires an accepted parent completion outcome")
	}
	candidate, err := s.LoadPublicationCandidate()
	if err != nil {
		return fmt.Errorf("publication invalidating finding requires current publication candidate: %w", err)
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if candidate.TaskID != taskID {
		return fmt.Errorf("publication invalidating finding candidate task %s does not match current task %s", candidate.TaskID, taskID)
	}
	if candidate.CommitOID != candidateOID || candidate.SnapshotID != snapshotID {
		return fmt.Errorf("publication invalidating finding target does not match current publication candidate")
	}
	finding := PublicationInvalidatingFinding{
		Version:             publicationInvalidatingFindingVersion,
		TaskID:              taskID,
		FindingID:           findingID,
		Disposition:         PublicationFindingCorrectnessDefect,
		CandidateCommitOID:  candidate.CommitOID,
		CandidateSnapshotID: candidate.SnapshotID,
		Origin:              origin,
		Cause:               cause,
	}
	if err := validatePublicationInvalidatingFinding(finding); err != nil {
		return err
	}
	existing, err := s.LoadPublicationInvalidatingFinding()
	if err == nil {
		if existing == finding {
			return nil
		}
		return fmt.Errorf("a different publication invalidating finding is already recorded")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(finding)
	if err != nil {
		return fmt.Errorf("publication invalidating finding cannot be encoded: %w", err)
	}
	return s.Write(publicationInvalidatingFindingStateFile, string(data))
}

func (s *StateStore) LoadPublicationInvalidatingFinding() (PublicationInvalidatingFinding, error) {
	data, err := s.Read(publicationInvalidatingFindingStateFile)
	if err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	var finding PublicationInvalidatingFinding
	if err := json.Unmarshal([]byte(data), &finding); err != nil {
		return PublicationInvalidatingFinding{}, fmt.Errorf("publication invalidating finding cannot be read: %w", err)
	}
	if err := validatePublicationInvalidatingFinding(finding); err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	return finding, nil
}

func (s *StateStore) CurrentPublicationInvalidatingFinding() (*PublicationInvalidatingFinding, error) {
	finding, err := s.LoadPublicationInvalidatingFinding()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return nil, err
	}
	if finding.TaskID != taskID {
		return nil, fmt.Errorf("publication invalidating finding task %s does not match current task %s", finding.TaskID, taskID)
	}
	candidate, err := s.LoadPublicationCandidate()
	if err != nil {
		return nil, fmt.Errorf("publication invalidating finding candidate is unavailable: %w", err)
	}
	if candidate.TaskID != taskID || finding.CandidateCommitOID != candidate.CommitOID || finding.CandidateSnapshotID != candidate.SnapshotID {
		return nil, fmt.Errorf("publication invalidating finding is stale or unrelated to the current publication candidate")
	}
	return &finding, nil
}

func (s *StateStore) ClearPublicationInvalidatingFinding() error {
	return s.Remove(publicationInvalidatingFindingStateFile)
}

func validatePublicationInvalidatingFinding(finding PublicationInvalidatingFinding) error {
	if finding.Version != publicationInvalidatingFindingVersion {
		return fmt.Errorf("unsupported publication invalidating finding version: %d", finding.Version)
	}
	if !ValidGeneratedUUID(finding.TaskID) || !ValidGeneratedUUID(finding.FindingID) {
		return fmt.Errorf("publication invalidating finding identity is invalid")
	}
	if finding.Disposition != PublicationFindingCorrectnessDefect {
		return fmt.Errorf("publication invalidating finding disposition is invalid: %s", finding.Disposition)
	}
	if !validPublicationOID(finding.CandidateCommitOID) || !validPublicationDigest(finding.CandidateSnapshotID) {
		return fmt.Errorf("publication invalidating finding candidate identity is invalid")
	}
	if err := validateParentFixDeclaration(finding.Origin, finding.Cause); err != nil {
		return err
	}
	return nil
}
