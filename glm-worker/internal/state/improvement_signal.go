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
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type ImprovementSignalDisposition string

type ImprovementSignalDispositionRecord struct {
	Version     int                          `json:"version"`
	TaskID      string                       `json:"task_id"`
	SignalKind  string                       `json:"signal_kind"`
	Disposition ImprovementSignalDisposition `json:"disposition"`
	TargetTask  string                       `json:"target_task,omitempty"`
	SignalCount int                          `json:"signal_count"`
	RecordedAt  time.Time                    `json:"recorded_at"`
}

type improvementSignalDispositionState struct {
	Version int                                  `json:"version"`
	Records []ImprovementSignalDispositionRecord `json:"records"`
}

const (
	ImprovementSignalStructuredRetryExhausted = "structured-retry-exhausted"
	ImprovementSignalSnapshotMismatch         = "snapshot-mismatch"

	ImprovementSignalDispositionAdopt           ImprovementSignalDisposition = "adopt"
	ImprovementSignalDispositionExistingOwner   ImprovementSignalDisposition = "existing-owner"
	ImprovementSignalDispositionDuplicate       ImprovementSignalDisposition = "duplicate"
	ImprovementSignalDispositionReject          ImprovementSignalDisposition = "reject"
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

func improvementDispositionNeedsTask(disposition ImprovementSignalDisposition) bool {
	switch disposition {
	case ImprovementSignalDispositionAdopt, ImprovementSignalDispositionExistingOwner, ImprovementSignalDispositionDuplicate:
		return true
	default:
		return false
	}
}

func (s *StateStore) PendingImprovementSignal() (*ImprovementSignal, error) {
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return nil, nil
	}
	records, err := s.CurrentImprovementSignalDispositions()
	if err != nil {
		return nil, err
	}
	disposed := make(map[string]bool, len(records))
	for _, record := range records {
		disposed[record.SignalKind] = true
	}
	stats, err := s.CurrentTaskStats()
	if err != nil || stats.TaskID != taskID {
		return nil, nil
	}
	candidates := []ImprovementSignal{
		{Kind: ImprovementSignalStructuredRetryExhausted, Count: stats.StructuredRetryExhausted},
		{Kind: ImprovementSignalSnapshotMismatch, Count: stats.SnapshotMismatches},
	}
	for _, signal := range candidates {
		if signal.Count > 0 && !disposed[signal.Kind] {
			candidate := signal
			return &candidate, nil
		}
	}
	return nil, nil
}

func (s *StateStore) RecordImprovementSignalDisposition(kind, disposition, targetTask string) (ImprovementSignalDispositionRecord, bool, error) {
	resolved := ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	if improvementDispositionNeedsTask(resolved) && targetTask == "" {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal disposition %s requires a target task", resolved)
	}
	if !improvementDispositionNeedsTask(resolved) && targetTask != "" {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal disposition %s does not accept a target task", resolved)
	}
	records, err := s.CurrentImprovementSignalDispositions()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	for _, record := range records {
		if record.SignalKind != kind {
			continue
		}
		if record.Disposition == resolved && record.TargetTask == targetTask {
			return record, false, nil
		}
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal %s already has disposition %s", kind, record.Disposition)
	}
	signal, err := s.PendingImprovementSignal()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	if signal == nil || signal.Kind != kind {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal %s is not the machine-required pending signal", kind)
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	record := ImprovementSignalDispositionRecord{
		Version:     improvementSignalDispositionVersion,
		TaskID:      taskID,
		SignalKind:  kind,
		Disposition: resolved,
		TargetTask:  targetTask,
		SignalCount: signal.Count,
		RecordedAt:  time.Now().UTC(),
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
		if improvementDispositionNeedsTask(record.Disposition) != (record.TargetTask != "") {
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
