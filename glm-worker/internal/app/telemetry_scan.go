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
	Status                 string               `json:"status"`
	Dir                    string               `json:"dir"`
	Files                  int                  `json:"files"`
	RecordsOutsidePeriod   int                  `json:"records_outside_period,omitempty"`
	RecordsUndatedExcluded int                  `json:"records_undated_excluded,omitempty"`
	IgnoredFiles           []string             `json:"ignored_files,omitempty"`
	UnreadableTasks        []telemetryTaskError `json:"unreadable_tasks,omitempty"`

	considered int
	logs       []state.TaskCallLogs
}

func scanTelemetryTaskLogs(st *state.StateStore, filter state.TelemetryQueryFilter) (*telemetryScan, error) {
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
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !filter.MatchesTask(taskID) {
			continue
		}
		scan.considered++
		if !state.ValidGeneratedUUID(taskID) {
			scan.IgnoredFiles = append(scan.IgnoredFiles, name)
			continue
		}
		logs, readErr := st.ReadModelCallLogs(taskID)
		if readErr == nil {
			scan.Files++
			logs = filterTelemetryLogsInPeriod(logs, filter, &scan.RecordsOutsidePeriod, &scan.RecordsUndatedExcluded)
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

func filterTelemetryLogsInPeriod(logs []state.ModelCallLog, filter state.TelemetryQueryFilter, outsidePeriod *int, undatedExcluded *int) []state.ModelCallLog {
	if !filter.HasPeriod() {
		return logs
	}
	filtered := make([]state.ModelCallLog, 0, len(logs))
	for _, log := range logs {
		if filter.ExcludesUndated(log.StartedAt) {
			*undatedExcluded++
			continue
		}
		if filter.CoversTime(log.StartedAt) {
			filtered = append(filtered, log)
			continue
		}
		*outsidePeriod++
	}
	return filtered
}
