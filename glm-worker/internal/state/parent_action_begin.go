package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type parentActionBeginRecord struct {
	Version      int        `json:"version"`
	TaskID       string     `json:"task_id"`
	SourceStatus TaskStatus `json:"source_status"`
}

const (
	parentActionBeginStateFile    = "parent-action-begin.json"
	parentActionBeginStateVersion = 1
)

var errNoParentActionBegin = errors.New("parent action begin transition is not pending")

func (s *StateStore) saveParentActionBegin(source TaskStatus) error {
	if source != TaskStatusWaitingDecision && source != TaskStatusWaitingSolReview {
		return fmt.Errorf("parent action begin source %s is not a parent waiting state", source)
	}
	if s.Exists(parentActionBeginStateFile) {
		return fmt.Errorf("parent action begin transition is already pending")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	record := parentActionBeginRecord{
		Version:      parentActionBeginStateVersion,
		TaskID:       taskID,
		SourceStatus: source,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("parent action begin recordをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(parentActionBeginStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("parent action begin recordを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) loadParentActionBegin() (parentActionBeginRecord, error) {
	data, err := os.ReadFile(s.Path(parentActionBeginStateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return parentActionBeginRecord{}, errNoParentActionBegin
		}
		return parentActionBeginRecord{}, err
	}
	var record parentActionBeginRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return parentActionBeginRecord{}, fmt.Errorf("parent action begin recordを読めません: %w", err)
	}
	if record.Version != parentActionBeginStateVersion {
		return parentActionBeginRecord{}, fmt.Errorf("unsupported parent action begin record version: %d", record.Version)
	}
	if record.TaskID == "" {
		return parentActionBeginRecord{}, fmt.Errorf("parent action begin record has no task identity")
	}
	if record.SourceStatus != TaskStatusWaitingDecision && record.SourceStatus != TaskStatusWaitingSolReview {
		return parentActionBeginRecord{}, fmt.Errorf("parent action begin record has invalid source status %s", record.SourceStatus)
	}
	return record, nil
}

func (s *StateStore) CommitParentActionBegin() error {
	record, err := s.loadParentActionBegin()
	if errors.Is(err, errNoParentActionBegin) {
		return nil
	}
	if err != nil {
		return err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	if record.TaskID != taskID {
		return fmt.Errorf("parent action begin task identity changed: record=%q current=%q", record.TaskID, taskID)
	}
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("parent action begin commit requires active task, got %s", s.TaskStatus())
	}
	return s.Remove(parentActionBeginStateFile)
}

func (s *StateStore) RecoverParentActionBeginFromState() (TaskStatus, error) {
	record, err := s.loadParentActionBegin()
	if err != nil {
		return TaskStatusNone, err
	}
	status, err := s.validateParentActionBeginRecovery(record)
	if err != nil {
		return TaskStatusNone, err
	}
	if err := s.clearParentActionBeginResume(record); err != nil {
		return TaskStatusNone, err
	}
	if status == TaskStatusActive {
		if err := s.SetTaskStatus(record.SourceStatus); err != nil {
			return TaskStatusNone, err
		}
	}
	if err := s.Remove(parentActionBeginStateFile); err != nil {
		return record.SourceStatus, err
	}
	return record.SourceStatus, nil
}

func (s *StateStore) validateParentActionBeginRecovery(record parentActionBeginRecord) (TaskStatus, error) {
	taskID, err := s.TaskID()
	if err != nil {
		return TaskStatusNone, err
	}
	if record.TaskID != taskID {
		return TaskStatusNone, fmt.Errorf("parent action recovery task identity changed: record=%q current=%q", record.TaskID, taskID)
	}
	status := s.TaskStatus()
	if status != TaskStatusActive && status != record.SourceStatus {
		return TaskStatusNone, fmt.Errorf("parent action recovery requires active task or retry of %s, got %s", record.SourceStatus, status)
	}
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return TaskStatusNone, fmt.Errorf("parent action recovery cannot read parent review state: %w", err)
	}
	if label != roundCommentNone {
		return TaskStatusNone, fmt.Errorf("parent action recovery requires no open parent review, got %s", label)
	}
	if err := s.validateParentActionBeginPayload(record.SourceStatus); err != nil {
		return TaskStatusNone, err
	}
	return status, nil
}

func (s *StateStore) validateParentActionBeginPayload(source TaskStatus) error {
	pending := s.Exists("pending-decision")
	if source == TaskStatusWaitingDecision && !pending {
		return fmt.Errorf("parent action recovery for a decision requires the pending decision payload")
	}
	if source == TaskStatusWaitingSolReview && pending {
		return fmt.Errorf("parent action recovery for a fix requires no pending decision")
	}
	return nil
}

func (s *StateStore) clearParentActionBeginResume(record parentActionBeginRecord) error {
	checkpoint, err := s.LoadResumeCheckpoint()
	if errors.Is(err, ErrNoResumeCheckpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	if checkpoint.IsStopped() || checkpoint.CompletedResult != nil || checkpoint.QualitySurfaceApprovalPending {
		return fmt.Errorf("parent action recovery cannot discard a checkpoint after model-call admission")
	}
	if checkpoint.Stage != ResumeStageWorker || checkpoint.Role != WorkerRole {
		return fmt.Errorf("parent action recovery checkpoint is not a worker pre-call checkpoint")
	}
	expectedPhase := WorkerPhaseCategoryExplicitFix
	if record.SourceStatus == TaskStatusWaitingDecision {
		expectedPhase = WorkerPhaseCategoryDecision
	}
	if WorkerPhaseCategory(checkpoint.Phase) != expectedPhase {
		return fmt.Errorf("parent action recovery checkpoint phase %s does not match source %s", checkpoint.Phase, record.SourceStatus)
	}
	return s.ClearResumeCheckpoint()
}
