package state

import (
	"encoding/json"
	"time"
)

type ParentActionAttemptRecord struct {
	Version    int       `json:"version"`
	AttemptID  string    `json:"attempt_id"`
	Action     string    `json:"action"`
	EnteredAt  time.Time `json:"entered_at"`
	ExitedAt   time.Time `json:"exited_at"`
	DurationMS int64     `json:"duration_ms"`
	Outcome    string    `json:"outcome"`
	Error      string    `json:"error,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
}

const (
	parentActionAttemptFile        = "parent-action-attempts.jsonl"
	parentActionAttemptRecordKeep  = 400
	parentActionAttemptErrorMaxLen = 256

	parentActionAttemptRecordVersion = 1

	ParentActionAttemptOutcomeSuccess = "success"
	ParentActionAttemptOutcomeFailed  = "failed"
)

func (s *StateStore) RecordParentActionAttempt(record ParentActionAttemptRecord) {
	record.Version = parentActionAttemptRecordVersion
	if record.ExitedAt.IsZero() && record.Outcome != "started" {
		record.ExitedAt = time.Now().UTC()
	}
	if len(record.Error) > parentActionAttemptErrorMaxLen {
		record.Error = record.Error[:parentActionAttemptErrorMaxLen]
	}
	data, err := json.Marshal(record)
	if err != nil {
		warnStatsFailure("parent action attempt telemetryのJSON化", err)
		return
	}
	if err := appendStateJSONL(s.Path(parentActionAttemptFile), data); err != nil {
		warnStatsFailure("parent action attempt telemetryの追記", err)
		return
	}
	s.pruneParentActionAttempts()
}

func (s *StateStore) pruneParentActionAttempts() {
	records, err := s.ReadParentActionAttempts()
	if err != nil || len(records) <= parentActionAttemptRecordKeep {
		return
	}
	kept := records[len(records)-parentActionAttemptRecordKeep:]
	var data []byte
	for _, record := range kept {
		line, err := json.Marshal(record)
		if err != nil {
			warnStatsFailure("parent action attempt telemetryの整理", err)
			return
		}
		data = append(append(data, line...), '\n')
	}
	if err := writeFileAtomic(s.Path(parentActionAttemptFile), data, 0o600); err != nil {
		warnStatsFailure("parent action attempt telemetryの整理", err)
	}
}

func (s *StateStore) ReadParentActionAttempts() ([]ParentActionAttemptRecord, error) {
	return readTimingRecords(s.Path(parentActionAttemptFile), parentActionAttemptRecordVersion, func(record ParentActionAttemptRecord) int { return record.Version })
}
