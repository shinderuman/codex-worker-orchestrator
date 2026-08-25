package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type callOutliersTelemetry struct {
	Status          string                   `json:"status"`
	Dir             string                   `json:"dir"`
	Files           int                      `json:"files"`
	IgnoredFiles    []string                 `json:"ignored_files,omitempty"`
	UnreadableTasks []callOutliersUnreadable `json:"unreadable_tasks,omitempty"`
}

type callOutliersUnreadable struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type callOutliersOutput struct {
	Telemetry callOutliersTelemetry   `json:"telemetry"`
	Report    state.CallOutlierReport `json:"report"`
}

func printCallOutliers(st *state.StateStore, stdout io.Writer) error {
	dir := st.Path("telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("telemetry dirを読めません: %w", err)
	}

	output := callOutliersOutput{
		Telemetry: callOutliersTelemetry{Status: "ok", Dir: dir},
	}
	taskLogs := make([]state.TaskCallLogs, 0, len(entries))
	considered := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		considered++
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !state.ValidGeneratedUUID(taskID) {
			output.Telemetry.IgnoredFiles = append(output.Telemetry.IgnoredFiles, name)
			continue
		}
		logs, readErr := st.ReadModelCallLogs(taskID)
		if readErr == nil {
			output.Telemetry.Files++
			taskLogs = append(taskLogs, state.TaskCallLogs{TaskID: taskID, Logs: logs})
			continue
		}
		output.Telemetry.Status = "partial"
		output.Telemetry.UnreadableTasks = append(output.Telemetry.UnreadableTasks, callOutliersUnreadable{
			TaskID: taskID,
			Error:  readErr.Error(),
		})
	}
	if considered == 0 {
		output.Telemetry.Status = "none"
		output.Report = state.BuildCallOutlierReport(nil)
		return writeJSON(stdout, output)
	}

	output.Report = state.BuildCallOutlierReport(taskLogs)
	return writeJSON(stdout, output)
}
