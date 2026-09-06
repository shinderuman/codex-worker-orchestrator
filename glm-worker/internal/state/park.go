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

func (s *StateStore) LeaveParked() (TaskStatus, error) {
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
	if err := s.SetTaskStatus(record.FromStatus); err != nil {
		return "", err
	}
	return record.FromStatus, nil
}
