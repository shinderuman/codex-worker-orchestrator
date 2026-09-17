package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type TaskDisposition string

type TaskDispositionRecord struct {
	Version     int             `json:"version"`
	TaskID      string          `json:"task_id"`
	FromStatus  string          `json:"from_status"`
	Disposition TaskDisposition `json:"disposition"`
	RecordedAt  time.Time       `json:"recorded_at"`
}

const (
	TaskDispositionCancel   TaskDisposition = "cancel"
	TaskDispositionAbandon  TaskDisposition = "abandon"
	TaskDispositionRecovery TaskDisposition = "recovery"

	taskDispositionStateFile = "task-disposition.json"
	taskDispositionVersion   = 1
)

func (d TaskDisposition) Valid() bool {
	switch d {
	case TaskDispositionCancel, TaskDispositionAbandon, TaskDispositionRecovery:
		return true
	default:
		return false
	}
}

func (s *StateStore) ResetWithDisposition(requested string) (TaskDisposition, error) {
	existing, existingErr := s.CurrentTaskDisposition()
	if existingErr != nil && !errors.Is(existingErr, os.ErrNotExist) {
		return "", existingErr
	}
	if existingErr == nil {
		if requested != "" && TaskDisposition(requested) != existing.Disposition {
			return "", fmt.Errorf("task reset disposition is already %s; cannot replace it with %s", existing.Disposition, requested)
		}
		if taskID := s.ReadOr("task.id", ""); taskID != "" && taskID != existing.TaskID {
			return "", fmt.Errorf("task reset disposition belongs to %s but current task is %s", existing.TaskID, taskID)
		}
		if err := s.ensureTaskDispositionLifecycle(existing); err != nil {
			return "", err
		}
		if err := s.Reset(); err != nil {
			return "", err
		}
		return existing.Disposition, nil
	}

	taskID := s.ReadOr("task.id", "")
	status := s.TaskStatus()
	if taskID == "" && status == TaskStatusNone {
		if requested != "" {
			return "", fmt.Errorf("cannot record reset disposition %s without a current task", requested)
		}
		return "", s.Reset()
	}
	if status == TaskStatusComplete {
		if requested != "" {
			return "", fmt.Errorf("completed task cleanup does not accept a reset disposition")
		}
		return "", s.Reset()
	}
	if taskID == "" {
		return "", fmt.Errorf("cannot dispose task state %s without task.id provenance", status)
	}

	disposition, err := resolveResetDisposition(status, requested)
	if err != nil {
		return "", err
	}
	record := TaskDispositionRecord{
		Version:     taskDispositionVersion,
		TaskID:      taskID,
		FromStatus:  string(status),
		Disposition: disposition,
		RecordedAt:  time.Now().UTC(),
	}
	if err := s.writeTaskDisposition(record); err != nil {
		return "", err
	}
	if err := s.ensureTaskDispositionLifecycle(record); err != nil {
		return "", err
	}
	if err := s.Reset(); err != nil {
		return "", err
	}
	return disposition, nil
}

func resolveResetDisposition(status TaskStatus, requested string) (TaskDisposition, error) {
	if requested == "" {
		if resetRecoveryStatus(status) {
			return TaskDispositionRecovery, nil
		}
		return "", fmt.Errorf("task status %s cannot be reset without explicit disposition; use --reset --disposition cancel or --reset --disposition abandon", status)
	}
	disposition := TaskDisposition(requested)
	if !disposition.Valid() {
		return "", fmt.Errorf("unknown task reset disposition %q", requested)
	}
	if disposition == TaskDispositionRecovery && status.Known() && status != TaskStatusNone && !resetRecoveryStatus(status) {
		return "", fmt.Errorf("task status %s is not a recovery reset; use cancel or abandon disposition", status)
	}
	return disposition, nil
}

func resetRecoveryStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusRateLimited,
		TaskStatusProviderUnavailable,
		TaskStatusInterrupted,
		TaskStatusGuardRecoverable,
		TaskStatusQualityGateRecoverable:
		return true
	default:
		return false
	}
}

func (s *StateStore) CurrentTaskDisposition() (TaskDispositionRecord, error) {
	data, err := os.ReadFile(s.Path(taskDispositionStateFile))
	if err != nil {
		return TaskDispositionRecord{}, err
	}
	return decodeTaskDisposition(data)
}

func decodeTaskDisposition(data []byte) (TaskDispositionRecord, error) {
	var record TaskDispositionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return TaskDispositionRecord{}, fmt.Errorf("task disposition is unreadable: %w", err)
	}
	if record.Version != taskDispositionVersion || record.TaskID == "" || record.FromStatus == "" || !record.Disposition.Valid() || record.RecordedAt.IsZero() {
		return TaskDispositionRecord{}, fmt.Errorf("task disposition is invalid")
	}
	return record, nil
}

func (s *StateStore) writeTaskDisposition(record TaskDispositionRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("task disposition cannot be encoded: %w", err)
	}
	if err := writeFileAtomic(s.Path(taskDispositionStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("task disposition cannot be written: %w", err)
	}
	return nil
}

func (s *StateStore) ValidateResetDispositionForNewTask() error {
	record, err := s.CurrentTaskDisposition()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("new task admission cannot verify reset disposition: %w", err)
	}
	if taskID := s.ReadOr("task.id", ""); taskID != "" && taskID != record.TaskID {
		return fmt.Errorf("new task admission found reset disposition for %s while current task is %s", record.TaskID, taskID)
	}
	if err := s.ensureTaskDispositionLifecycle(record); err != nil {
		return fmt.Errorf("new task admission cannot verify reset lifecycle: %w", err)
	}
	evidence, err := s.ArchivedTaskStatsEvidence(record.TaskID)
	if err == nil && evidence.Proven && string(evidence.Status) != record.FromStatus {
		return fmt.Errorf("reset disposition status %s does not match archived task status %s", record.FromStatus, evidence.Status)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("new task admission cannot verify archived reset task stats: %w", err)
	}
	return nil
}

func (s *StateStore) ensureTaskDispositionLifecycle(record TaskDispositionRecord) error {
	records, err := ReadTaskLifecycle(s.TaskLifecycleLogPath(record.TaskID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("task disposition lifecycle is unreadable: %w", err)
	}
	for _, lifecycle := range records {
		if lifecycle.Disposition == "" {
			continue
		}
		if lifecycle.Disposition == record.Disposition && lifecycle.From == record.FromStatus && lifecycle.To == string(TaskStatusNone) && lifecycle.Timestamp.Equal(record.RecordedAt) {
			return nil
		}
		return fmt.Errorf("task %s already has a conflicting lifecycle disposition", record.TaskID)
	}
	if err := s.AppendTaskLifecycle(TaskLifecycleRecord{
		TaskID:      record.TaskID,
		Timestamp:   record.RecordedAt,
		From:        record.FromStatus,
		To:          string(TaskStatusNone),
		Disposition: record.Disposition,
	}); err != nil {
		return fmt.Errorf("task disposition lifecycle cannot be recorded: %w", err)
	}
	return nil
}
