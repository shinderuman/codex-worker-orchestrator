package report

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type repoSearchEvalOutput struct {
	Events    testImpactEventsScan   `json:"events"`
	Telemetry TelemetryScan          `json:"telemetry"`
	Rounds    modelRoutingRoundsScan `json:"rounds"`
	RepoRoot  string                 `json:"repo_root"`
	Report    state.RepoSearchReport `json:"report"`
}

func PrintRepoSearchEval(st *state.StateStore, stdout io.Writer) error {
	events, err := scanTaskEventLogs(st)
	if err != nil {
		return err
	}
	scan, err := ScanTelemetryTaskLogs(st, state.TelemetryQueryFilter{})
	if err != nil {
		return err
	}
	rounds, tasks := attachModelRoutingConvergenceDeltas(st, scan.Logs)
	stats, err := st.AllTaskStats()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, repoSearchEvalOutput{
		Events:    *events,
		Telemetry: *scan,
		Rounds:    rounds,
		RepoRoot:  st.ReadOr("repo-root", ""),
		Report: state.BuildRepoSearchReportWithCompleteness(
			events.logs,
			repoSearchStatsByTask(stats),
			testImpactReviews(tasks),
			events.incompleteTasks,
		),
	})
}

func repoSearchStatsByTask(all []state.TaskStats) map[string]state.TaskStats {
	byTask := make(map[string]state.TaskStats, len(all))
	for _, stats := range all {
		byTask[stats.TaskID] = stats
	}
	return byTask
}
