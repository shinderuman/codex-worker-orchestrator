package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	GuardRepairParentActionEnv    = "GLM_WORKER_PARENT_ACTION"
	GuardRepairParentActionResume = "resume"
	GuardRepairRebuiltResume      = "guard-repair-resume"
)

type GuardRepairStatus string

const (
	GuardRepairRequested GuardRepairStatus = "requested"
	GuardRepairRunning   GuardRepairStatus = "running"
	GuardRepairReady     GuardRepairStatus = "ready"
	GuardRepairFailed    GuardRepairStatus = "failed"
	GuardRepairComplete  GuardRepairStatus = "complete"
)

type GuardRepairRecord struct {
	Version        int               `json:"version"`
	TaskID         string            `json:"task_id"`
	Phase          string            `json:"phase"`
	Fingerprint    string            `json:"fingerprint"`
	Strategy       string            `json:"strategy"`
	Status         GuardRepairStatus `json:"status"`
	Failure        string            `json:"failure"`
	RelevantDigest string            `json:"relevant_digest,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

const (
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

func (s *StateStore) RequestGuardRepair(record GuardRepairRecord) error {
	existing, err := s.LoadGuardRepairRecord()
	if err == nil && existing.TaskID == record.TaskID && existing.Fingerprint == record.Fingerprint &&
		existing.Strategy == record.Strategy && existing.RelevantDigest == record.RelevantDigest {
		return nil
	}
	if err != nil && !errors.Is(err, ErrNoGuardRepairRecord) {
		return err
	}
	record.Status = GuardRepairRequested
	return s.SaveGuardRepairRecord(record)
}

func (s *StateStore) SaveGuardRepairRecord(record GuardRepairRecord) error {
	if record.TaskID == "" || record.Phase == "" || record.Fingerprint == "" || record.Strategy == "" || record.Failure == "" || record.RelevantDigest == "" {
		return fmt.Errorf("guard repair record identity is incomplete")
	}
	if !record.Status.Valid() {
		return fmt.Errorf("guard repair record has invalid status %q", record.Status)
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
	if record.TaskID == "" || record.Phase == "" || record.Fingerprint == "" || record.Strategy == "" || record.Failure == "" || record.RelevantDigest == "" || !record.Status.Valid() {
		return GuardRepairRecord{}, fmt.Errorf("guard repair record is incomplete")
	}
	return record, nil
}

func (s *StateStore) ClearGuardRepairRecord() error {
	return s.Remove(guardRepairStateFile)
}
