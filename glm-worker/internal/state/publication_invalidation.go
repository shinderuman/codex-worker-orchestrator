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

type publicationInvalidatingFindingTarget struct {
	taskID    string
	candidate PublicationCandidate
}

const (
	publicationInvalidatingFindingStateFile = "publication-invalidating-finding.json"
	publicationInvalidatingFindingVersion   = 1
	PublicationFindingCorrectnessDefect     = "correctness-defect"
)

func (s *StateStore) RecordPublicationInvalidatingFinding(origin, cause string) (PublicationInvalidatingFinding, error) {
	if err := validateParentFixDeclaration(origin, cause); err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	target, err := s.publicationInvalidatingFindingTarget()
	if err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	existing, reusable, err := s.reusablePublicationInvalidatingFinding(target, origin, cause)
	if err != nil || reusable {
		return existing, err
	}
	return s.writePublicationInvalidatingFinding(target, origin, cause)
}

func (s *StateStore) publicationInvalidatingFindingTarget() (publicationInvalidatingFindingTarget, error) {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion {
		return publicationInvalidatingFindingTarget{}, fmt.Errorf("publication invalidating finding requires %s, got %s", TaskStatusAwaitingParentCompletion, s.TaskStatus())
	}
	completion, err := s.CurrentParentCompletionOutcome()
	if err != nil {
		return publicationInvalidatingFindingTarget{}, err
	}
	if completion == nil || completion.Terminal != SessionRotationTerminalAccept {
		return publicationInvalidatingFindingTarget{}, fmt.Errorf("publication invalidating finding requires an accepted parent completion outcome")
	}
	candidate, err := s.LoadPublicationCandidate()
	if err != nil {
		return publicationInvalidatingFindingTarget{}, fmt.Errorf("publication invalidating finding requires current publication candidate: %w", err)
	}
	taskID, err := s.TaskID()
	if err != nil {
		return publicationInvalidatingFindingTarget{}, err
	}
	if candidate.TaskID != taskID {
		return publicationInvalidatingFindingTarget{}, fmt.Errorf("publication invalidating finding candidate task %s does not match current task %s", candidate.TaskID, taskID)
	}
	return publicationInvalidatingFindingTarget{taskID: taskID, candidate: candidate}, nil
}

func (s *StateStore) reusablePublicationInvalidatingFinding(target publicationInvalidatingFindingTarget, origin, cause string) (PublicationInvalidatingFinding, bool, error) {
	existing, err := s.LoadPublicationInvalidatingFinding()
	if errors.Is(err, os.ErrNotExist) {
		return PublicationInvalidatingFinding{}, false, nil
	}
	if err != nil {
		return PublicationInvalidatingFinding{}, false, err
	}
	candidate := target.candidate
	if existing.TaskID == target.taskID &&
		existing.Disposition == PublicationFindingCorrectnessDefect &&
		existing.CandidateCommitOID == candidate.CommitOID &&
		existing.CandidateSnapshotID == candidate.SnapshotID &&
		existing.Origin == origin && existing.Cause == cause {
		return existing, true, nil
	}
	return PublicationInvalidatingFinding{}, false, fmt.Errorf("a different publication invalidating finding is already recorded")
}

func (s *StateStore) writePublicationInvalidatingFinding(target publicationInvalidatingFindingTarget, origin, cause string) (PublicationInvalidatingFinding, error) {
	findingID, err := NewUUID()
	if err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	finding := PublicationInvalidatingFinding{
		Version:             publicationInvalidatingFindingVersion,
		TaskID:              target.taskID,
		FindingID:           findingID,
		Disposition:         PublicationFindingCorrectnessDefect,
		CandidateCommitOID:  target.candidate.CommitOID,
		CandidateSnapshotID: target.candidate.SnapshotID,
		Origin:              origin,
		Cause:               cause,
	}
	if err := validatePublicationInvalidatingFinding(finding); err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	data, err := json.Marshal(finding)
	if err != nil {
		return PublicationInvalidatingFinding{}, fmt.Errorf("publication invalidating finding cannot be encoded: %w", err)
	}
	if err := s.Write(publicationInvalidatingFindingStateFile, string(data)); err != nil {
		return PublicationInvalidatingFinding{}, err
	}
	return finding, nil
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
