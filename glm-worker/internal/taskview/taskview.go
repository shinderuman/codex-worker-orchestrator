package taskview

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
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

var ErrEventLogReadIncomplete = errors.New("task event log read incomplete")

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

func LastTaskEvent(st *state.StateStore, taskID string) (state.TaskEventRecord, bool, error) {
	if taskID == "" {
		return state.TaskEventRecord{}, false, nil
	}
	return readLastTaskEvent(st.TaskEventLogPath(taskID), taskID)
}

func readLastTaskEvent(path, taskID string) (state.TaskEventRecord, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state.TaskEventRecord{}, false, nil
	}
	if err != nil {
		return state.TaskEventRecord{}, false, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var last state.TaskEventRecord
	found := false
	var tailErr error
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			tailErr = err
			continue
		}
		if record.TaskID != taskID {
			tailErr = fmt.Errorf("task event ID does not match the requested task")
			continue
		}
		last, found, tailErr = record, true, nil
	}
	if err := scanner.Err(); err != nil {
		tailErr = err
	}
	if tailErr != nil {
		return state.TaskEventRecord{}, false, fmt.Errorf("%w: %w", ErrEventLogReadIncomplete, tailErr)
	}
	return last, found, nil
}

func MarshalEventLine(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
