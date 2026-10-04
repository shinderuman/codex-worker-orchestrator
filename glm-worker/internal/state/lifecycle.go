package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TaskLifecycleRecord struct {
	Version   int       `json:"version"`
	TaskID    string    `json:"task_id"`
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
	To        string    `json:"to"`
}

const taskLifecycleLogVersion = 1

func (s *StateStore) AppendTaskLifecycle(record TaskLifecycleRecord) error {
	if record.Version == 0 {
		record.Version = taskLifecycleLogVersion
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("task lifecycleをJSON化できません: %w", err)
	}
	return appendStateJSONL(s.TaskLifecycleLogPath(record.TaskID), data)
}

func (s *StateStore) TaskLifecycleLogPath(taskID string) string {
	return s.Path(filepath.Join("lifecycle", taskID+".jsonl"))
}

func ParseTaskLifecycleLine(data []byte) (TaskLifecycleRecord, error) {
	var record TaskLifecycleRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return TaskLifecycleRecord{}, fmt.Errorf("task lifecycleを読めません: %w", err)
	}
	if record.Version != taskLifecycleLogVersion {
		return TaskLifecycleRecord{}, fmt.Errorf("unsupported task lifecycle version: %d", record.Version)
	}
	return record, nil
}

func WarnTaskLifecycleSkip(err error) {
	writeStatsWarningEvent("lifecycle_log", "task lifecycle境界の追記に失敗したため観測をskipします（task本体へ影響しません）", err)
}

func ReadTaskLifecycle(path string) ([]TaskLifecycleRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var result []TaskLifecycleRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		record, err := ParseTaskLifecycleLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
