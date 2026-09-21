package report

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type statsOutputWithArchiveScan struct {
	StatsOutput
	TaskStatsArchiveScan state.TaskStatsArchiveScan `json:"task_stats_archive_scan"`
}

// PrintStatsWithArchiveScan preserves the existing stats aggregate while
// surfacing bounded coverage for task-stats archives that were considered but
// rejected as unsupported machine schema/revision.
func PrintStatsWithArchiveScan(cfg config.AppConfig, st *state.StateStore, query Query, compact CompactSummaryFunc, stdout io.Writer) error {
	if query.Compact {
		return compact(cfg, st, query, stdout)
	}
	if query.IsHistory() {
		return printStatsHistory(st, query, stdout)
	}

	result, err := st.AllTaskStatsWithArchiveScan()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, statsOutputWithArchiveScan{
		StatsOutput:           buildStatsOutput(st, result.Stats, query),
		TaskStatsArchiveScan: result.ArchiveScan,
	})
}
