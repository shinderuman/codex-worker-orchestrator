package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type callOutliersOutput struct {
	Telemetry telemetryScan           `json:"telemetry"`
	Report    state.CallOutlierReport `json:"report"`
}

func printCallOutliers(st *state.StateStore, stdout io.Writer) error {
	scan, err := scanTelemetryTaskLogs(st)
	if err != nil {
		return err
	}
	return writeJSON(stdout, callOutliersOutput{
		Telemetry: *scan,
		Report:    state.BuildCallOutlierReport(scan.logs),
	})
}
