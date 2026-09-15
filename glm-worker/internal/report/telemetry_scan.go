package report

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type telemetryTaskError struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type TelemetryScan struct {
	Status                 string               `json:"status"`
	Dir                    string               `json:"dir"`
	Files                  int                  `json:"files"`
	RecordsOutsidePeriod   int                  `json:"records_outside_period,omitempty"`
	RecordsUndatedExcluded int                  `json:"records_undated_excluded,omitempty"`
	IgnoredFiles           []string             `json:"ignored_files,omitempty"`
	UnreadableTasks        []telemetryTaskError `json:"unreadable_tasks,omitempty"`

	considered int
	Logs       []state.TaskCallLogs `json:"-"`
}

func ScanTelemetryTaskLogs(st *state.StateStore, filter state.TelemetryQueryFilter) (*TelemetryScan, error) {
	current, err := st.ScanTelemetryCurrent(filter)
	if err != nil {
		return nil, err
	}

	scan := &TelemetryScan{
		Status:                 "ok",
		Dir:                    current.Dir,
		Files:                  current.Files,
		RecordsOutsidePeriod:   current.RecordsOutsidePeriod,
		RecordsUndatedExcluded: current.RecordsUndatedExcluded,
		IgnoredFiles:           current.IgnoredFiles,
		considered:             current.FilesConsidered,
		Logs:                   current.Logs,
	}
	for _, unreadable := range current.UnreadableTasks {
		scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
			TaskID: unreadable.TaskID,
			Error:  unreadable.Error,
		})
	}
	if len(scan.UnreadableTasks) > 0 {
		scan.Status = taskview.StatusPartial
	}
	if scan.considered == 0 {
		scan.Status = taskview.StatusNone
	}
	return scan, nil
}
