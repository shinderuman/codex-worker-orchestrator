package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type GuardRepairStatus string

type GuardRepairIntegration struct {
	RepositoryBoundary GitSnapshot                  `json:"repository_boundary"`
	StopDirtyFiles     []StopDirtyFile              `json:"stop_dirty_files"`
	Files              []GuardRepairIntegrationFile `json:"files"`
}

type GuardRepairRecord struct {
	Version                int                     `json:"version"`
	TaskID                 string                  `json:"task_id"`
	Phase                  string                  `json:"phase"`
	Fingerprint            string                  `json:"fingerprint"`
	Strategy               string                  `json:"strategy"`
	Status                 GuardRepairStatus       `json:"status"`
	Failure                string                  `json:"failure"`
	RelevantDigest         string                  `json:"relevant_digest"`
	RepairedDigest         string                  `json:"repaired_digest,omitempty"`
	Integration            *GuardRepairIntegration `json:"integration,omitempty"`
	ResumeAttemptID        string                  `json:"resume_attempt_id,omitempty"`
	ResumeCheckpointDigest string                  `json:"resume_checkpoint_digest,omitempty"`
	OriginalResumeObserved bool                    `json:"original_resume_observed,omitempty"`
	UpdatedAt              time.Time               `json:"updated_at"`
}

const (
	GuardRepairParentActionEnv    = "GLM_WORKER_PARENT_ACTION"
	GuardRepairParentActionResume = "resume"
	GuardRepairRebuiltResume      = "guard-repair-resume"

	GuardRepairRequested   GuardRepairStatus = "requested"
	GuardRepairRunning     GuardRepairStatus = "running"
	GuardRepairIntegrating GuardRepairStatus = "integrating"
	GuardRepairReady       GuardRepairStatus = "ready"
	GuardRepairResuming    GuardRepairStatus = "resuming"
	GuardRepairFailed      GuardRepairStatus = "failed"
	GuardRepairComplete    GuardRepairStatus = "complete"

	guardRepairStateFile    = "guard-repair.json"
	guardRepairStateVersion = 2
)

var ErrNoGuardRepairRecord = errors.New("guard repair record is not available")

func (status GuardRepairStatus) Valid() bool {
	switch status {
	case GuardRepairRequested, GuardRepairRunning, GuardRepairIntegrating, GuardRepairReady,
		GuardRepairResuming, GuardRepairFailed, GuardRepairComplete:
		return true
	default:
		return false
	}
}

func (record GuardRepairRecord) validate() error {
	if record.TaskID == "" || record.Phase == "" || record.Fingerprint == "" || record.Strategy == "" {
		return fmt.Errorf("guard repair record identity is incomplete")
	}
	if record.Failure == "" || record.RelevantDigest == "" {
		return fmt.Errorf("guard repair record evidence is incomplete")
	}
	if !record.Status.Valid() {
		return fmt.Errorf("guard repair record has invalid status %q", record.Status)
	}
	return record.validateProgress()
}

func (record GuardRepairRecord) validateProgress() error {
	if err := record.validateRepairProgress(); err != nil {
		return err
	}
	return record.validateResumeProgress()
}

func (record GuardRepairRecord) validateRepairProgress() error {
	repaired := record.Status == GuardRepairReady || record.Status == GuardRepairResuming || record.Status == GuardRepairComplete
	if repaired && record.RepairedDigest == "" {
		return fmt.Errorf("repaired guard repair record requires repaired digest")
	}
	if record.Status != GuardRepairIntegrating {
		if record.Integration != nil {
			return fmt.Errorf("guard repair integration rollback state requires integrating status")
		}
		return nil
	}
	if record.Integration == nil {
		return fmt.Errorf("integrating guard repair record requires integration rollback state")
	}
	return record.Integration.validate()
}

func (record GuardRepairRecord) validateResumeProgress() error {
	resumeProof := record.ResumeAttemptID != "" || record.ResumeCheckpointDigest != "" || record.OriginalResumeObserved
	if record.Status != GuardRepairResuming && record.Status != GuardRepairComplete {
		if resumeProof {
			return fmt.Errorf("guard repair resume proof requires resuming or complete status")
		}
		return nil
	}
	if !ValidGeneratedUUID(record.ResumeAttemptID) || record.ResumeCheckpointDigest == "" {
		return fmt.Errorf("guard repair resume proof is incomplete")
	}
	if record.Status == GuardRepairComplete && !record.OriginalResumeObserved {
		return fmt.Errorf("complete guard repair record requires original resume evidence")
	}
	return nil
}

func (record *GuardRepairRecord) ClearResumeProof() {
	record.ResumeAttemptID = ""
	record.ResumeCheckpointDigest = ""
	record.OriginalResumeObserved = false
}

func (s *StateStore) RequestGuardRepair(record GuardRepairRecord) error {
	existing, err := s.LoadGuardRepairRecord()
	if err == nil && existing.sameRecovery(record) {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNoGuardRepairRecord) {
		return err
	}
	record.Status = GuardRepairRequested
	return s.SaveGuardRepairRecord(record)
}

func (record GuardRepairRecord) sameRecovery(next GuardRepairRecord) bool {
	if record.TaskID != next.TaskID || record.Strategy != next.Strategy {
		return false
	}
	if record.Fingerprint == next.Fingerprint && record.RelevantDigest == next.RelevantDigest {
		return true
	}
	repaired := record.Status == GuardRepairReady || record.Status == GuardRepairResuming || record.Status == GuardRepairComplete
	return repaired && record.Phase == next.Phase && record.Failure == next.Failure &&
		record.RepairedDigest != "" && record.RepairedDigest == next.RelevantDigest
}

func (s *StateStore) SaveGuardRepairRecord(record GuardRepairRecord) error {
	if err := record.validate(); err != nil {
		return err
	}
	record.Version = guardRepairStateVersion
	record.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("guard repair recordをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(guardRepairStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("guard repair recordを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) LoadGuardRepairRecord() (GuardRepairRecord, error) {
	data, err := os.ReadFile(s.Path(guardRepairStateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GuardRepairRecord{}, ErrNoGuardRepairRecord
		}
		return GuardRepairRecord{}, err
	}
	var record GuardRepairRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return GuardRepairRecord{}, fmt.Errorf("guard repair recordを読めません: %w", err)
	}
	if record.Version != guardRepairStateVersion {
		return GuardRepairRecord{}, fmt.Errorf("unsupported guard repair record version: %d", record.Version)
	}
	if err := record.validate(); err != nil {
		return GuardRepairRecord{}, err
	}
	return record, nil
}
