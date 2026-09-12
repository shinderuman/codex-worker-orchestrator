package state

import (
	"errors"
	"fmt"
	"os"
)

type ParkRecord struct {
	Version           int                  `json:"version"`
	ParkID            string               `json:"park_id"`
	CreatedAt         string               `json:"created_at"`
	FromStatus        TaskStatus           `json:"from_status"`
	TaskID            string               `json:"task_id"`
	WorkerSessionID   string               `json:"worker_session_id,omitempty"`
	ReviewerSessionID string               `json:"reviewer_session_id,omitempty"`
	RepoRoot          string               `json:"repo_root"`
	Head              string               `json:"head"`
	Snapshot          GitSnapshot          `json:"snapshot"`
	DirtyFiles        []StopDirtyFile      `json:"dirty_files"`
	ParentFiles       *ParentFileStates    `json:"parent_files,omitempty"`
	Baseline          *GitBaselineEvidence `json:"baseline,omitempty"`
	Worktree          string               `json:"worktree"`
	Branch            string               `json:"branch"`
	InterruptTaskID   string               `json:"interrupt_task_id,omitempty"`
	Cleanup           *ParkCleanup         `json:"cleanup,omitempty"`
}

type ParkCleanup struct {
	Integration string      `json:"integration"`
	Snapshot    GitSnapshot `json:"snapshot"`
	BranchTip   string      `json:"branch_tip"`
}

type ParkOrigin struct {
	Version   int    `json:"version"`
	ParkID    string `json:"park_id"`
	RepoRoot  string `json:"repo_root"`
	TaskID    string `json:"task_id"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"created_at"`
}

const (
	parkStateFile     = "park.json"
	parkOriginFile    = "park.origin.json"
	parkContentDir    = "park-content"
	parkRecordVersion = 1
)

var ErrNoParkRecord = errors.New("parked task record is not available")

func ParkAdmissibleFrom(status TaskStatus) bool {
	return status == TaskStatusWaitingSolReview || status == TaskStatusWaitingDecision
}

func (s *StateStore) SaveParkRecord(record ParkRecord) error {
	record.Version = parkRecordVersion
	return writeJSONStateFile(s.Path(parkStateFile), record)
}

func (s *StateStore) LoadParkRecord() (ParkRecord, error) {
	var record ParkRecord
	if err := readJSONStateFile(s.Path(parkStateFile), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ParkRecord{}, ErrNoParkRecord
		}
		return ParkRecord{}, err
	}
	if record.Version != parkRecordVersion {
		return ParkRecord{}, fmt.Errorf("unsupported park record version: %d", record.Version)
	}
	return record, nil
}

func (s *StateStore) SaveParkOrigin(origin ParkOrigin) error {
	origin.Version = parkRecordVersion
	return writeJSONStateFile(s.Path(parkOriginFile), origin)
}

func (s *StateStore) ClearParkRecord() error {
	return s.Remove(parkStateFile)
}

func (s *StateStore) RemoveParkContent() error {
	if err := os.RemoveAll(s.Path(parkContentDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("park本文保持directoryを削除できません: %w", err)
	}
	return nil
}

func (s *StateStore) RemoveParkOrigin() error {
	return s.Remove(parkOriginFile)
}

func (s *StateStore) LoadParkOrigin() (ParkOrigin, error) {
	var origin ParkOrigin
	if err := readJSONStateFile(s.Path(parkOriginFile), &origin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ParkOrigin{}, ErrNoParkRecord
		}
		return ParkOrigin{}, err
	}
	if origin.Version != parkRecordVersion {
		return ParkOrigin{}, fmt.Errorf("unsupported park origin version: %d", origin.Version)
	}
	return origin, nil
}

func (s *StateStore) ParkContentPath(relative string) string {
	return s.Path(joinParkContent(relative))
}

func joinParkContent(relative string) string {
	return parkContentDir + "/" + relative
}

func (s *StateStore) EnterParked(record ParkRecord) error {
	if !ParkAdmissibleFrom(s.TaskStatus()) {
		return fmt.Errorf("park transition requires waiting-sol-review or waiting-decision, got %s", s.TaskStatus())
	}
	record.FromStatus = s.TaskStatus()
	record.TaskID = s.ReadOr("task.id", "")
	if err := s.SaveParkRecord(record); err != nil {
		return err
	}
	return s.SetTaskStatus(TaskStatusParked)
}

func (s *StateStore) CommitUnpark() (TaskStatus, error) {
	if s.TaskStatus() != TaskStatusParked {
		return "", fmt.Errorf("unpark transition requires parked task, got %s", s.TaskStatus())
	}
	record, err := s.LoadParkRecord()
	if err != nil {
		return "", err
	}
	if record.FromStatus != TaskStatusWaitingSolReview && record.FromStatus != TaskStatusWaitingDecision {
		return "", fmt.Errorf("park record from-status %s is not unparkable", record.FromStatus)
	}
	if record.Cleanup == nil {
		return "", fmt.Errorf("unpark cleanup checkpoint is missing")
	}
	if err := s.SetTaskStatus(record.FromStatus); err != nil {
		return "", err
	}
	return record.FromStatus, nil
}

func (s *StateStore) CompleteUnpark() error {
	record, err := s.LoadParkRecord()
	if err != nil {
		return err
	}
	if record.Cleanup == nil {
		return fmt.Errorf("unpark cleanup checkpoint is missing")
	}
	if s.TaskStatus() != record.FromStatus {
		return fmt.Errorf("unpark cleanup completion requires restored status %s, got %s", record.FromStatus, s.TaskStatus())
	}
	return s.ClearParkRecord()
}

func (s *StateStore) PendingUnparkCleanup() (ParkRecord, bool, error) {
	status := s.TaskStatus()
	if status != TaskStatusWaitingSolReview && status != TaskStatusWaitingDecision {
		return ParkRecord{}, false, nil
	}
	record, err := s.LoadParkRecord()
	if errors.Is(err, ErrNoParkRecord) {
		return ParkRecord{}, false, nil
	}
	if err != nil {
		return ParkRecord{}, false, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ParkRecord{}, false, err
	}
	if record.TaskID != taskID {
		return ParkRecord{}, false, fmt.Errorf("unpark cleanup record task %s does not match current task", record.TaskID)
	}
	if record.Cleanup == nil {
		return ParkRecord{}, false, fmt.Errorf("waiting task has a park record without an unpark cleanup checkpoint")
	}
	if record.FromStatus != status {
		return ParkRecord{}, false, fmt.Errorf("unpark cleanup origin %s does not match current status %s", record.FromStatus, status)
	}
	return record, true, nil
}
