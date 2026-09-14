package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type parentActionBeginSnapshot struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
}

type parentActionBeginRecord struct {
	Version      int                       `json:"version"`
	TaskID       string                    `json:"task_id"`
	SourceStatus TaskStatus                `json:"source_status"`
	Pending      parentActionBeginSnapshot `json:"pending_decision"`
	Review       parentActionBeginSnapshot `json:"parent_review"`
}

const (
	parentActionBeginStateFile    = "parent-action-begin.json"
	parentActionBeginStateVersion = 1
)

var errNoParentActionBegin = errors.New("parent action begin transition is not pending")

func (s *StateStore) saveParentActionBegin(rollback ParentActionRollback) error {
	if rollback.status != TaskStatusWaitingDecision && rollback.status != TaskStatusWaitingSolReview {
		return fmt.Errorf("parent action begin source %s is not a parent waiting state", rollback.status)
	}
	if rollback.status == TaskStatusWaitingDecision && !rollback.pending.exists {
		return fmt.Errorf("parent action begin decision source has no pending decision snapshot")
	}
	if rollback.status == TaskStatusWaitingSolReview && rollback.pending.exists {
		return fmt.Errorf("parent action begin fix source has an unexpected pending decision snapshot")
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
		SourceStatus: rollback.status,
		Pending:      parentActionSnapshotFromLifecycle(rollback.pending),
		Review:       parentActionSnapshotFromLifecycle(rollback.review),
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

func parentActionSnapshotFromLifecycle(snapshot lifecycleFileSnapshot) parentActionBeginSnapshot {
	return parentActionBeginSnapshot{
		Exists: snapshot.exists,
		Data:   append([]byte(nil), snapshot.data...),
	}
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
	if err := validateParentActionBeginRecord(record); err != nil {
		return parentActionBeginRecord{}, err
	}
	return record, nil
}

func validateParentActionBeginRecord(record parentActionBeginRecord) error {
	if record.Version != parentActionBeginStateVersion {
		return fmt.Errorf("unsupported parent action begin record version: %d", record.Version)
	}
	if record.TaskID == "" {
		return fmt.Errorf("parent action begin record has no task identity")
	}
	if err := validateParentActionBeginSource(record); err != nil {
		return err
	}
	if err := validateParentActionBeginSnapshotShape(record); err != nil {
		return err
	}
	return validateParentActionBeginReviewSnapshot(record)
}

func validateParentActionBeginSource(record parentActionBeginRecord) error {
	switch record.SourceStatus {
	case TaskStatusWaitingDecision:
		if !record.Pending.Exists {
			return fmt.Errorf("parent action begin decision record has no pending decision snapshot")
		}
	case TaskStatusWaitingSolReview:
		if record.Pending.Exists {
			return fmt.Errorf("parent action begin fix record has an unexpected pending decision snapshot")
		}
	default:
		return fmt.Errorf("parent action begin record has invalid source status %s", record.SourceStatus)
	}
	return nil
}

func validateParentActionBeginSnapshotShape(record parentActionBeginRecord) error {
	if !record.Pending.Exists && len(record.Pending.Data) != 0 {
		return fmt.Errorf("parent action begin record has data for a missing pending decision snapshot")
	}
	if !record.Review.Exists && len(record.Review.Data) != 0 {
		return fmt.Errorf("parent action begin record has data for a missing review snapshot")
	}
	return nil
}

func validateParentActionBeginReviewSnapshot(record parentActionBeginRecord) error {
	if !record.Review.Exists {
		return nil
	}
	var review ParentReviewState
	if err := json.Unmarshal(record.Review.Data, &review); err != nil {
		return fmt.Errorf("parent action begin review snapshotを読めません: %w", err)
	}
	if review.Version != parentReviewStateVersion || review.TaskID != record.TaskID {
		return fmt.Errorf("parent action begin review snapshotのschemaが不正です")
	}
	if review.Open != nil && !validParentReviewPacketStatus(review.Open.PacketStatus) {
		return fmt.Errorf("parent action begin review snapshotのpacket statusが不正です: %s", review.Open.PacketStatus)
	}
	if err := validateParentReviewBindingState(review); err != nil {
		return err
	}
	return validateParentCompletionState(review)
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
	if err := s.restoreParentActionBeginSnapshots(record); err != nil {
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
	if err := s.validateParentActionBeginCurrentSnapshots(record); err != nil {
		return TaskStatusNone, err
	}
	return status, nil
}

func (s *StateStore) validateParentActionBeginCurrentSnapshots(record parentActionBeginRecord) error {
	pending, err := s.snapshotLifecycleFile("pending-decision")
	if err != nil {
		return err
	}
	if !sameParentActionSnapshot(pending, record.Pending) {
		return fmt.Errorf("parent action recovery pending decision no longer matches the begin transaction")
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	if sameParentActionSnapshot(review, record.Review) {
		return nil
	}
	if !review.exists || !record.Review.Exists {
		return fmt.Errorf("parent action recovery review state no longer matches the begin transaction")
	}
	current, err := s.loadParentReviewState()
	if err != nil {
		return err
	}
	if current.Open != nil || current.Review != nil {
		return fmt.Errorf("parent action recovery review state no longer matches the begin transaction")
	}
	return nil
}

func sameParentActionSnapshot(current lifecycleFileSnapshot, saved parentActionBeginSnapshot) bool {
	return current.exists == saved.Exists && bytes.Equal(current.data, saved.Data)
}

func (s *StateStore) restoreParentActionBeginSnapshots(record parentActionBeginRecord) error {
	if err := s.restoreLifecycleFile(lifecycleFileSnapshot{
		name:   parentReviewStateFile,
		data:   append([]byte(nil), record.Review.Data...),
		exists: record.Review.Exists,
	}); err != nil {
		return err
	}
	if err := s.restoreLifecycleFile(lifecycleFileSnapshot{
		name:   "pending-decision",
		data:   append([]byte(nil), record.Pending.Data...),
		exists: record.Pending.Exists,
	}); err != nil {
		return err
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
