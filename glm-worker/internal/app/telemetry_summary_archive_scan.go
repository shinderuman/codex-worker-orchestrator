package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type telemetryCompactSummaryWithArchiveScan struct {
	telemetryCompactSummary
	TaskStatsArchiveScan state.TaskStatsArchiveScan `json:"task_stats_archive_scan"`
}

func printTelemetryCompactSummaryWithArchiveScan(cfg config.AppConfig, st *state.StateStore, query report.Query, stdout io.Writer) error {
	summary, err := buildTelemetryCompactSummary(cfg, st, query)
	if err != nil {
		return err
	}
	scan, err := st.ScanTaskStatsArchives()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, telemetryCompactSummaryWithArchiveScan{
		telemetryCompactSummary: summary,
		TaskStatsArchiveScan:    scan,
	})
}
