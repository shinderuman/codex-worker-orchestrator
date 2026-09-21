package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

type ImprovementSignal struct {
	Kind         string `json:"kind"`
	Count        int    `json:"count"`
	SourceCallID string `json:"source_call_id,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type ImprovementSignalDisposition string

type ImprovementSignalDispositionRecord struct {
	Version      int                          `json:"version"`
	TaskID       string                       `json:"task_id"`
	SignalKind   string                       `json:"signal_kind"`
	Disposition  ImprovementSignalDisposition `json:"disposition"`
	TargetTask   string                       `json:"target_task,omitempty"`
	SignalCount  int                          `json:"signal_count"`
	SourceCallID string                       `json:"source_call_id,omitempty"`
	RecordedAt   time.Time                    `json:"recorded_at"`
}

type improvementSignalDispositionState struct {
	Version int                                  `json:"version"`
	Records []ImprovementSignalDispositionRecord `json:"records"`
}

const (
	ImprovementSignalInvalidPacket = "invalid-packet"

	ImprovementSignalDispositionAdopt            ImprovementSignalDisposition = "adopt"
	ImprovementSignalDispositionExistingOwner    ImprovementSignalDisposition = "existing-owner"
	ImprovementSignalDispositionDuplicate        ImprovementSignalDisposition = "duplicate"
	ImprovementSignalDispositionReject           ImprovementSignalDisposition = "reject"
	ImprovementSignalDispositionAwaitingEvidence ImprovementSignalDisposition = "awaiting-evidence"

	improvementSignalDispositionStateFile = "improvement-signal-dispositions.json"
	improvementSignalDispositionVersion   = 1
)

func ImprovementSignalDispositionChoices() []string {
	return []string{
		string(ImprovementSignalDispositionAdopt),
		string(ImprovementSignalDispositionExistingOwner),
		string(ImprovementSignalDispositionDuplicate),
		string(ImprovementSignalDispositionReject),
		string(ImprovementSignalDispositionAwaitingEvidence),
	}
}

func (d ImprovementSignalDisposition) Valid() bool {
	switch d {
	case ImprovementSignalDispositionAdopt,
		ImprovementSignalDispositionExistingOwner,
		ImprovementSignalDispositionDuplicate,
		ImprovementSignalDispositionReject,
		ImprovementSignalDispositionAwaitingEvidence:
		return true
	default:
		return false
	}
}

func ImprovementDispositionNeedsTask(disposition ImprovementSignalDisposition) bool {
	switch disposition {
	case ImprovementSignalDispositionAdopt, ImprovementSignalDispositionExistingOwner, ImprovementSignalDispositionDuplicate:
		return true
	default:
		return false
	}
}

func (s *StateStore) ImprovementSignalDisposed(kind string) (bool, error) {
	records, err := s.CurrentImprovementSignalDispositions()
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.SignalKind == kind {
			return true, nil
		}
	}
	return false, nil
}

func (s *StateStore) RecordImprovementSignalDisposition(signal ImprovementSignal, disposition, targetTask string) (ImprovementSignalDispositionRecord, bool, error) {
	if signal.Kind == "" || signal.Count <= 0 {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal is invalid")
	}
	resolved := ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	if ImprovementDispositionNeedsTask(resolved) && targetTask == "" {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal disposition %s requires a target task", resolved)
	}
	if !ImprovementDispositionNeedsTask(resolved) && targetTask != "" {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal disposition %s does not accept a target task", resolved)
	}
	records, err := s.CurrentImprovementSignalDispositions()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	for _, record := range records {
		if record.SignalKind != signal.Kind {
			continue
		}
		if record.Disposition == resolved && record.TargetTask == targetTask {
			return record, false, nil
		}
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal %s already has disposition %s", signal.Kind, record.Disposition)
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	record := ImprovementSignalDispositionRecord{
		Version:      improvementSignalDispositionVersion,
		TaskID:       taskID,
		SignalKind:   signal.Kind,
		Disposition:  resolved,
		TargetTask:   targetTask,
		SignalCount:  signal.Count,
		SourceCallID: signal.SourceCallID,
		RecordedAt:   time.Now().UTC(),
	}
	records = append(records, record)
	if err := s.saveImprovementSignalDispositions(records); err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	return record, true, nil
}

func (s *StateStore) CurrentImprovementSignalDispositions() ([]ImprovementSignalDispositionRecord, error) {
	data, err := s.Read(improvementSignalDispositionStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored improvementSignalDispositionState
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return nil, fmt.Errorf("improvement signal disposition state cannot be read: %w", err)
	}
	if stored.Version != improvementSignalDispositionVersion || len(stored.Records) == 0 {
		return nil, fmt.Errorf("improvement signal disposition state is invalid")
	}
	taskID := s.ReadOr("task.id", "")
	seen := make(map[string]bool, len(stored.Records))
	records := append([]ImprovementSignalDispositionRecord(nil), stored.Records...)
	for _, record := range records {
		if record.Version != improvementSignalDispositionVersion || record.TaskID != taskID || record.SignalKind == "" || !record.Disposition.Valid() || record.SignalCount <= 0 || record.RecordedAt.IsZero() {
			return nil, fmt.Errorf("improvement signal disposition record is invalid")
		}
		if ImprovementDispositionNeedsTask(record.Disposition) != (record.TargetTask != "") {
			return nil, fmt.Errorf("improvement signal disposition target task is invalid")
		}
		if seen[record.SignalKind] {
			return nil, fmt.Errorf("improvement signal disposition is duplicated for %s", record.SignalKind)
		}
		seen[record.SignalKind] = true
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SignalKind < records[j].SignalKind })
	return records, nil
}

func (s *StateStore) saveImprovementSignalDispositions(records []ImprovementSignalDispositionRecord) error {
	if len(records) == 0 {
		return fmt.Errorf("cannot save empty improvement signal disposition state")
	}
	records = append([]ImprovementSignalDispositionRecord(nil), records...)
	sort.Slice(records, func(i, j int) bool { return records[i].SignalKind < records[j].SignalKind })
	data, err := json.Marshal(improvementSignalDispositionState{Version: improvementSignalDispositionVersion, Records: records})
	if err != nil {
		return fmt.Errorf("improvement signal disposition state cannot be encoded: %w", err)
	}
	return s.Write(improvementSignalDispositionStateFile, string(data))
}
