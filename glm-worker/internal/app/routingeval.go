package app

import (
	"errors"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type modelRoutingOutput struct {
	Telemetry telemetryScan            `json:"telemetry"`
	Rounds    modelRoutingRoundsScan   `json:"rounds"`
	RepoRoot  string                   `json:"repo_root"`
	Report    state.ModelRoutingReport `json:"report"`
}

type modelRoutingRoundsScan struct {
	Status          string               `json:"status"`
	Dir             string               `json:"dir"`
	UnreadableTasks []telemetryTaskError `json:"unreadable_tasks,omitempty"`
}

func printModelRouting(st *state.StateStore, stdout io.Writer) error {
	scan, err := scanTelemetryTaskLogs(st)
	if err != nil {
		return err
	}
	rounds, tasks := attachModelRoutingConvergenceDeltas(st, scan.logs)
	return writeJSON(stdout, modelRoutingOutput{
		Telemetry: *scan,
		Rounds:    rounds,
		RepoRoot:  st.ReadOr("repo-root", ""),
		Report:    state.BuildModelRoutingReport(tasks),
	})
}

func attachModelRoutingConvergenceDeltas(st *state.StateStore, tasks []state.TaskCallLogs) (modelRoutingRoundsScan, []state.TaskCallLogs) {
	scan := modelRoutingRoundsScan{Status: statusNone, Dir: st.Path("rounds")}
	readable := 0
	for index := range tasks {
		records, _, err := readRoundRecords(st, tasks[index].TaskID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				scan.Status = statusPartial
				scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
					TaskID: tasks[index].TaskID,
					Error:  err.Error(),
				})
			}
			continue
		}
		readable++
		tasks[index].ConvergenceDeltas = convergenceCallDeltas(records, tasks[index].Logs)
	}
	if readable > 0 && scan.Status != statusPartial {
		scan.Status = "ok"
	}
	return scan, tasks
}

func convergenceCallDeltas(records []state.RoundRecord, logs []state.ModelCallLog) map[string]string {
	rounds, _ := buildConvergenceRounds(records, logs)
	deltas := make(map[string]string)
	for _, round := range rounds {
		for _, entry := range round.reviewer {
			recordConvergenceCallDelta(deltas, entry, round.delta.Class)
		}
		for _, entry := range round.worker {
			recordConvergenceCallDelta(deltas, entry, round.delta.Class)
		}
	}
	return deltas
}

func recordConvergenceCallDelta(deltas map[string]string, entry state.ModelCallLog, class string) {
	if entry.CallID == "" {
		return
	}
	deltas[entry.CallID] = class
}
