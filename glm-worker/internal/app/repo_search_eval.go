package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type repoSearchEvalOutput struct {
	Events    testImpactEventsScan   `json:"events"`
	Telemetry telemetryScan          `json:"telemetry"`
	Rounds    modelRoutingRoundsScan `json:"rounds"`
	RepoRoot  string                 `json:"repo_root"`
	Report    state.RepoSearchReport `json:"report"`
}

func printRepoSearchEval(st *state.StateStore, stdout io.Writer) error {
	events, err := scanTaskEventLogs(st)
	if err != nil {
		return err
	}
	scan, err := scanTelemetryTaskLogs(st, state.TelemetryQueryFilter{})
	if err != nil {
		return err
	}
	rounds, tasks := attachModelRoutingConvergenceDeltas(st, scan.logs)
	stats, err := st.AllTaskStats()
	if err != nil {
		return err
	}
	return writeJSON(stdout, repoSearchEvalOutput{
		Events:    *events,
		Telemetry: *scan,
		Rounds:    rounds,
		RepoRoot:  st.ReadOr("repo-root", ""),
		Report:    state.BuildRepoSearchReport(events.logs, repoSearchStatsByTask(stats), testImpactReviews(tasks)),
	})
}

func repoSearchStatsByTask(all []state.TaskStats) map[string]state.TaskStats {
	byTask := make(map[string]state.TaskStats, len(all))
	for _, stats := range all {
		byTask[stats.TaskID] = stats
	}
	return byTask
}
