package report

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type statsOutputWithArchiveScan struct {
	StatsOutput
	TaskStatsArchiveScan state.TaskStatsArchiveScan `json:"task_stats_archive_scan"`
}

func PrintStatsWithArchiveScan(st *state.StateStore, query Query, stdout io.Writer) error {
	if query.IsHistory() {
		return PrintStats(st, query, stdout)
	}

	result, err := st.AllTaskStatsWithArchiveScan()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, statsOutputWithArchiveScan{
		StatsOutput:          buildStatsOutput(st, result.Stats, query),
		TaskStatsArchiveScan: result.ArchiveScan,
	})
}
