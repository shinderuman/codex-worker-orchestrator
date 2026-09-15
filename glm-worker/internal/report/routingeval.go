package report

import (
	"errors"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type modelRoutingOutput struct {
	Telemetry TelemetryScan            `json:"telemetry"`
	Rounds    modelRoutingRoundsScan   `json:"rounds"`
	RepoRoot  string                   `json:"repo_root"`
	Report    state.ModelRoutingReport `json:"report"`
}

type modelRoutingRoundsScan struct {
	Status          string               `json:"status"`
	Dir             string               `json:"dir"`
	UnreadableTasks []telemetryTaskError `json:"unreadable_tasks,omitempty"`
}

func PrintModelRouting(st *state.StateStore, stdout io.Writer) error {
	scan, err := ScanTelemetryTaskLogs(st, state.TelemetryQueryFilter{})
	if err != nil {
		return err
	}
	rounds, tasks := attachModelRoutingConvergenceDeltas(st, scan.Logs)
	return machinecli.WriteJSON(stdout, modelRoutingOutput{
		Telemetry: *scan,
		Rounds:    rounds,
		RepoRoot:  st.ReadOr("repo-root", ""),
		Report:    state.BuildModelRoutingReport(tasks),
	})
}

func attachModelRoutingConvergenceDeltas(st *state.StateStore, tasks []state.TaskCallLogs) (modelRoutingRoundsScan, []state.TaskCallLogs) {
	scan := modelRoutingRoundsScan{Status: taskview.StatusNone, Dir: st.Path("rounds")}
	readable := 0
	for index := range tasks {
		records, _, err := readRoundRecords(st, tasks[index].TaskID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				scan.Status = taskview.StatusPartial
				scan.UnreadableTasks = append(scan.UnreadableTasks, telemetryTaskError{
					TaskID: tasks[index].TaskID,
					Error:  err.Error(),
				})
			}
			continue
		}
		readable++
		tasks[index].ConvergenceDeltas = convergenceCallDeltas(records, tasks[index].Logs)
		tasks[index].QualityOutcomes = ConvergenceQualityOutcomes(records, tasks[index].Logs)
	}
	if readable > 0 && scan.Status != taskview.StatusPartial {
		scan.Status = "ok"
	}
	return scan, tasks
}

func convergenceCallDeltas(records []state.RoundRecord, logs []state.ModelCallLog) map[string]string {
	rounds, _ := BuildConvergenceRounds(records, logs)
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

func ConvergenceQualityOutcomes(records []state.RoundRecord, logs []state.ModelCallLog) map[string]string {
	rounds, _ := BuildConvergenceRounds(records, logs)
	outcomes := make(map[string]string)
	for _, round := range rounds {
		if round.gap || round.mismatch || len(round.worker) != 1 ||
			round.worker[0].PacketStatus != string(packet.StatusImplemented) {
			continue
		}
		review := ConvergenceReviewOutDetail(round)
		if review.Outcome == nil {
			continue
		}
		quality := ""
		switch *review.Outcome {
		case string(packet.StatusPass):
			quality = state.ModelRoutingQualityReviewPass
		case string(packet.StatusFixRequired):
			quality = state.ModelRoutingQualityReviewFixRequired
		default:
			continue
		}
		if callID := round.worker[0].CallID; callID != "" {
			outcomes[callID] = quality
		}
	}
	return outcomes
}

func recordConvergenceCallDelta(deltas map[string]string, entry state.ModelCallLog, class string) {
	if entry.CallID == "" {
		return
	}
	deltas[entry.CallID] = class
}
