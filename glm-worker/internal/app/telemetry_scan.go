package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

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
	current, err := st.ScanTelemetryCurrent(filter)
	if err != nil {
		return nil, err
	}

	scan := &telemetryScan{
		Status:                 "ok",
		Dir:                    current.Dir,
		Files:                  current.Files,
		RecordsOutsidePeriod:   current.RecordsOutsidePeriod,
		RecordsUndatedExcluded: current.RecordsUndatedExcluded,
		IgnoredFiles:           current.IgnoredFiles,
		considered:             current.FilesConsidered,
		logs:                   current.Logs,
	}
	for _, unreadable := range current.UnreadableTasks {
		scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
			TaskID: unreadable.TaskID,
			Error:  unreadable.Error,
		})
	}
	if len(scan.UnreadableTasks) > 0 {
		scan.Status = statusPartial
	}
	if scan.considered == 0 {
		scan.Status = statusNone
	}
	return scan, nil
}
