package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type GuardRepairStatus string

type GuardRepairRecord struct {
	Version                int               `json:"version"`
	TaskID                 string            `json:"task_id"`
	Phase                  string            `json:"phase"`
	Fingerprint            string            `json:"fingerprint"`
	Strategy               string            `json:"strategy"`
	Status                 GuardRepairStatus `json:"status"`
	Failure                string            `json:"failure"`
	RelevantDigest         string            `json:"relevant_digest"`
	RepairedDigest         string            `json:"repaired_digest,omitempty"`
	OriginalResumeObserved bool              `json:"original_resume_observed,omitempty"`
	UpdatedAt              time.Time         `json:"updated_at"`
}

const (
	GuardRepairParentActionEnv    = "GLM_WORKER_PARENT_ACTION"
	GuardRepairParentActionResume = "resume"
	GuardRepairRebuiltResume      = "guard-repair-resume"

	GuardRepairRequested GuardRepairStatus = "requested"
	GuardRepairRunning   GuardRepairStatus = "running"
	GuardRepairReady     GuardRepairStatus = "ready"
	GuardRepairFailed    GuardRepairStatus = "failed"
	GuardRepairComplete  GuardRepairStatus = "complete"

	guardRepairStateFile    = "guard-repair.json"
	guardRepairStateVersion = 1
)

var ErrNoGuardRepairRecord = errors.New("guard repair record is not available")

func (status GuardRepairStatus) Valid() bool {
	switch status {
	case GuardRepairRequested, GuardRepairRunning, GuardRepairReady, GuardRepairFailed, GuardRepairComplete:
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
	if err := record.validateCompletion(); err != nil {
		return err
	}
	return nil
}

func (record GuardRepairRecord) validateCompletion() error {
	ready := record.Status == GuardRepairReady || record.Status == GuardRepairComplete
	if ready && record.RepairedDigest == "" {
		return fmt.Errorf("ready guard repair record requires repaired digest")
	}
	if record.Status == GuardRepairComplete && !record.OriginalResumeObserved {
		return fmt.Errorf("complete guard repair record requires original resume evidence")
	}
	if record.OriginalResumeObserved && record.Status != GuardRepairComplete {
		return fmt.Errorf("original resume evidence requires complete guard repair status")
	}
	return nil
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
	ready := record.Status == GuardRepairReady || record.Status == GuardRepairComplete
	return ready && record.Phase == next.Phase && record.Failure == next.Failure &&
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
