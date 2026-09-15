package taskview

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type EventLogSkippedLine struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

const StatusNone = "none"

const StatusPartial = "partial"

const StatusUnreadable = "unreadable"

func ReadStatusTelemetry(st *state.StateStore, taskID string) ([]state.ModelCallLog, error) {
	if taskID == "" {
		return nil, nil
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return logs, nil
}

func LastTaskEvent(st *state.StateStore, taskID string) (state.TaskEventRecord, bool) {
	if taskID == "" {
		return state.TaskEventRecord{}, false
	}
	return ReadLastTaskEvent(st.TaskEventLogPath(taskID))
}

func ReadLastTaskEvent(path string) (state.TaskEventRecord, bool) {
	file, err := os.Open(path)
	if err != nil {
		return state.TaskEventRecord{}, false
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var last state.TaskEventRecord
	found := false
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			continue
		}
		last = record
		found = true
	}
	return last, found
}

func MarshalEventLine(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
