package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type parentReopenTransactionSnapshot struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
}

type parentReopenTransactionRecord struct {
	Version      int                               `json:"version"`
	TaskID       string                            `json:"task_id"`
	SourceStatus TaskStatus                        `json:"source_status"`
	Snapshots    []parentReopenTransactionSnapshot `json:"snapshots"`
}

const (
	parentReopenTransactionStateFile = "parent-reopen-transaction.json"
	parentReopenTransactionVersion   = 1
)

var errNoParentReopenTransaction = errors.New("parent reopen transition is not pending")

func parentReopenSnapshotStateFiles() []string {
	return []string{
		parentReviewStateFile,
		"task.status",
		publicationCandidateStateFile,
		runtimeInstallEvidenceFile,
		publicationReopenLineageStateFile,
		publicationInvalidatingFindingStateFile,
	}
}

func (s *StateStore) saveParentReopenTransaction(snapshots []lifecycleFileSnapshot) error {
	if s.Exists(parentReopenTransactionStateFile) {
		return fmt.Errorf("parent reopen transition is already pending")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return err
	}
	record := parentReopenTransactionRecord{
		Version:      parentReopenTransactionVersion,
		TaskID:       taskID,
		SourceStatus: TaskStatusAwaitingParentCompletion,
		Snapshots:    make([]parentReopenTransactionSnapshot, 0, len(snapshots)),
	}
	for _, snapshot := range snapshots {
		record.Snapshots = append(record.Snapshots, parentReopenTransactionSnapshot{
			Name:   snapshot.name,
			Exists: snapshot.exists,
			Data:   append([]byte(nil), snapshot.data...),
		})
	}
	if err := validateParentReopenTransactionRecord(record); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("parent reopen transaction recordをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(parentReopenTransactionStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("parent reopen transaction recordを書き込めません: %w", err)
	}
	return nil
}

func (s *StateStore) loadParentReopenTransaction() (parentReopenTransactionRecord, error) {
	data, err := os.ReadFile(s.Path(parentReopenTransactionStateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return parentReopenTransactionRecord{}, errNoParentReopenTransaction
		}
		return parentReopenTransactionRecord{}, err
	}
	var record parentReopenTransactionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return parentReopenTransactionRecord{}, fmt.Errorf("parent reopen transaction recordを読めません: %w", err)
	}
	if err := validateParentReopenTransactionRecord(record); err != nil {
		return parentReopenTransactionRecord{}, err
	}
	return record, nil
}

func validateParentReopenTransactionRecord(record parentReopenTransactionRecord) error {
	if record.Version != parentReopenTransactionVersion {
		return fmt.Errorf("unsupported parent reopen transaction version: %d", record.Version)
	}
	if record.TaskID == "" {
		return fmt.Errorf("parent reopen transaction has no task identity")
	}
	if record.SourceStatus != TaskStatusAwaitingParentCompletion {
		return fmt.Errorf("parent reopen transaction has invalid source status %s", record.SourceStatus)
	}
	expected := parentReopenSnapshotStateFiles()
	if len(record.Snapshots) != len(expected) {
		return fmt.Errorf("parent reopen transaction has %d snapshots, want %d", len(record.Snapshots), len(expected))
	}
	for index, snapshot := range record.Snapshots {
		if snapshot.Name != expected[index] {
			return fmt.Errorf("parent reopen transaction snapshot %d is %q, want %q", index, snapshot.Name, expected[index])
		}
		if !snapshot.Exists && len(snapshot.Data) != 0 {
			return fmt.Errorf("parent reopen transaction snapshot %s has data for a missing state file", snapshot.Name)
		}
	}
	status := record.Snapshots[1]
	if !status.Exists || TaskStatus(strings.TrimSpace(string(status.Data))) != record.SourceStatus {
		return fmt.Errorf("parent reopen transaction task status snapshot does not match source status %s", record.SourceStatus)
	}
	return nil
}

func (s *StateStore) RecoverInterruptedParentReopen() (bool, error) {
	record, err := s.loadParentReopenTransaction()
	if errors.Is(err, errNoParentReopenTransaction) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	taskID, err := s.TaskID()
	if err != nil {
		return true, err
	}
	if record.TaskID != taskID {
		return true, fmt.Errorf("parent reopen recovery task identity changed: record=%q current=%q", record.TaskID, taskID)
	}

	var restoreErr error
	for index := len(record.Snapshots) - 1; index >= 0; index-- {
		snapshot := record.Snapshots[index]
		if err := s.restoreLifecycleFile(lifecycleFileSnapshot{
			name:   snapshot.Name,
			data:   append([]byte(nil), snapshot.Data...),
			exists: snapshot.Exists,
		}); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	if restoreErr != nil {
		return true, fmt.Errorf("parent reopen recovery could not restore source state: %w", restoreErr)
	}
	if _, _, err := s.reopenableParentCompletion(); err != nil {
		return true, fmt.Errorf("parent reopen recovery restored invalid source state: %w", err)
	}
	if _, err := s.LoadPublicationCandidate(); err != nil {
		return true, fmt.Errorf("parent reopen recovery restored no usable publication candidate: %w", err)
	}
	if err := s.Remove(parentReopenTransactionStateFile); err != nil {
		return true, fmt.Errorf("parent reopen recovery could not clear transaction record: %w", err)
	}
	return true, nil
}

func (s *StateStore) rollbackParentReopenTransaction(cause error) error {
	recovered, recoveryErr := s.RecoverInterruptedParentReopen()
	if recoveryErr != nil {
		return fmt.Errorf("parent reopen transition failed and recovery failed: transition=%w recovery=%w", cause, recoveryErr)
	}
	if !recovered {
		return fmt.Errorf("parent reopen transition failed without a durable recovery record: %w", cause)
	}
	return cause
}
