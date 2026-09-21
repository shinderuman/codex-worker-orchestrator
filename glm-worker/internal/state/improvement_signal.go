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

	ParentActionImprovementDisposition ParentAction = "improvement-disposition"

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

func (s *StateStore) PendingImprovementSignal() (*ImprovementSignal, error) {
	disposed, err := s.ImprovementSignalDisposed(ImprovementSignalInvalidPacket)
	if err != nil || disposed {
		return nil, err
	}
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return nil, nil
	}
	logs, err := s.ReadModelCallLogs(taskID)
	if errors.Is(err, os.ErrNotExist) || err != nil {
		return nil, nil
	}
	return invalidPacketSignalFromLogs(logs), nil
}

func invalidPacketSignalFromLogs(logs []ModelCallLog) *ImprovementSignal {
	count := 0
	var latest ModelCallLog
	for _, log := range logs {
		if log.CallType != CallTypeTask || log.Outcome != "invalid_packet" {
			continue
		}
		count++
		latest = log
	}
	if count == 0 {
		return nil
	}
	reason := latest.PacketRejectReason
	if reason == "" {
		reason = ImprovementSignalInvalidPacket
	}
	return &ImprovementSignal{
		Kind:         ImprovementSignalInvalidPacket,
		Count:        count,
		SourceCallID: latest.CallID,
		Reason:       reason,
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

func (s *StateStore) RecordImprovementSignalDisposition(kind, disposition, targetTask string) (ImprovementSignalDispositionRecord, bool, error) {
	resolved, err := validateImprovementDispositionRequest(disposition, targetTask)
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	records, err := s.CurrentImprovementSignalDispositions()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	if existing, found, err := matchingImprovementDisposition(records, kind, resolved, targetTask); found || err != nil {
		return existing, false, err
	}
	signal, err := s.PendingImprovementSignal()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	if signal == nil || signal.Kind != kind {
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal %s is not the current machine-visible pending signal", kind)
	}
	record, err := s.newImprovementDispositionRecord(*signal, resolved, targetTask)
	if err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	records = append(records, record)
	if err := s.saveImprovementSignalDispositions(records); err != nil {
		return ImprovementSignalDispositionRecord{}, false, err
	}
	return record, true, nil
}

func validateImprovementDispositionRequest(disposition, targetTask string) (ImprovementSignalDisposition, error) {
	resolved := ImprovementSignalDisposition(disposition)
	if !resolved.Valid() {
		return "", fmt.Errorf("unknown improvement signal disposition %q", disposition)
	}
	needsTask := ImprovementDispositionNeedsTask(resolved)
	if needsTask && targetTask == "" {
		return "", fmt.Errorf("improvement signal disposition %s requires a target task", resolved)
	}
	if !needsTask && targetTask != "" {
		return "", fmt.Errorf("improvement signal disposition %s does not accept a target task", resolved)
	}
	return resolved, nil
}

func matchingImprovementDisposition(records []ImprovementSignalDispositionRecord, kind string, disposition ImprovementSignalDisposition, targetTask string) (ImprovementSignalDispositionRecord, bool, error) {
	for _, record := range records {
		if record.SignalKind != kind {
			continue
		}
		if record.Disposition == disposition && record.TargetTask == targetTask {
			return record, true, nil
		}
		return ImprovementSignalDispositionRecord{}, false, fmt.Errorf("improvement signal %s already has disposition %s", kind, record.Disposition)
	}
	return ImprovementSignalDispositionRecord{}, false, nil
}

func (s *StateStore) newImprovementDispositionRecord(signal ImprovementSignal, disposition ImprovementSignalDisposition, targetTask string) (ImprovementSignalDispositionRecord, error) {
	taskID, err := s.TaskID()
	if err != nil {
		return ImprovementSignalDispositionRecord{}, err
	}
	return ImprovementSignalDispositionRecord{
		Version:      improvementSignalDispositionVersion,
		TaskID:       taskID,
		SignalKind:   signal.Kind,
		Disposition:  disposition,
		TargetTask:   targetTask,
		SignalCount:  signal.Count,
		SourceCallID: signal.SourceCallID,
		RecordedAt:   time.Now().UTC(),
	}, nil
}

func (s *StateStore) CurrentImprovementSignalDispositions() ([]ImprovementSignalDispositionRecord, error) {
	data, err := s.Read(improvementSignalDispositionStateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stored, err := decodeImprovementDispositionState(data)
	if err != nil {
		return nil, err
	}
	records := append([]ImprovementSignalDispositionRecord(nil), stored.Records...)
	if err := validateImprovementDispositionRecords(records, s.ReadOr("task.id", "")); err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SignalKind < records[j].SignalKind })
	return records, nil
}

func decodeImprovementDispositionState(data string) (improvementSignalDispositionState, error) {
	var stored improvementSignalDispositionState
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return improvementSignalDispositionState{}, fmt.Errorf("improvement signal disposition state cannot be read: %w", err)
	}
	if stored.Version != improvementSignalDispositionVersion || len(stored.Records) == 0 {
		return improvementSignalDispositionState{}, fmt.Errorf("improvement signal disposition state is invalid")
	}
	return stored, nil
}

func validateImprovementDispositionRecords(records []ImprovementSignalDispositionRecord, taskID string) error {
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if err := validateImprovementDispositionRecord(record, taskID); err != nil {
			return err
		}
		if seen[record.SignalKind] {
			return fmt.Errorf("improvement signal disposition is duplicated for %s", record.SignalKind)
		}
		seen[record.SignalKind] = true
	}
	return nil
}

func validateImprovementDispositionRecord(record ImprovementSignalDispositionRecord, taskID string) error {
	if record.Version != improvementSignalDispositionVersion || record.TaskID != taskID || record.SignalKind == "" || !record.Disposition.Valid() || record.SignalCount <= 0 || record.RecordedAt.IsZero() {
		return fmt.Errorf("improvement signal disposition record is invalid")
	}
	if ImprovementDispositionNeedsTask(record.Disposition) != (record.TargetTask != "") {
		return fmt.Errorf("improvement signal disposition target task is invalid")
	}
	return nil
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
