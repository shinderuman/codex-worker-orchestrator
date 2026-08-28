package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type telemetryTaskError struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type telemetryScan struct {
	Status          string               `json:"status"`
	Dir             string               `json:"dir"`
	Files           int                  `json:"files"`
	IgnoredFiles    []string             `json:"ignored_files,omitempty"`
	UnreadableTasks []telemetryTaskError `json:"unreadable_tasks,omitempty"`

	considered int
	logs       []state.TaskCallLogs
}

func scanTelemetryTaskLogs(st *state.StateStore) (*telemetryScan, error) {
	dir := st.Path("telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("telemetry dirを読めません: %w", err)
	}

	scan := &telemetryScan{Status: "ok", Dir: dir}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		scan.considered++
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !state.ValidGeneratedUUID(taskID) {
			scan.IgnoredFiles = append(scan.IgnoredFiles, name)
			continue
		}
		logs, readErr := st.ReadModelCallLogs(taskID)
		if readErr == nil {
			scan.Files++
			scan.logs = append(scan.logs, state.TaskCallLogs{TaskID: taskID, Logs: logs})
			continue
		}
		scan.Status = statusPartial
		scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
			TaskID: taskID,
			Error:  readErr.Error(),
		})
	}
	if scan.considered == 0 {
		scan.Status = statusNone
	}
	return scan, nil
}
