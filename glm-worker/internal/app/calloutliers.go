package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type callOutliersOutput struct {
	Query     telemetryQueryView      `json:"query"`
	Telemetry telemetryScan           `json:"telemetry"`
	Report    state.CallOutlierReport `json:"report"`
}

type callOutliersHistoryReport struct {
	Version        int                     `json:"version"`
	SchemaRevision int                     `json:"schema_revision"`
	Report         state.CallOutlierReport `json:"report"`
}

type callOutliersHistoryOutput struct {
	Query     telemetryQueryView          `json:"query"`
	Telemetry state.TelemetryHistoryScan  `json:"telemetry"`
	Reports   []callOutliersHistoryReport `json:"reports"`
}

func printCallOutliers(st *state.StateStore, query TelemetryQueryArgs, stdout io.Writer) error {
	if query.isHistory() {
		return printCallOutliersHistory(st, query, stdout)
	}
	scan, err := scanTelemetryTaskLogs(st, query.Filter)
	if err != nil {
		return err
	}
	return writeJSON(stdout, callOutliersOutput{
		Query:     query.view(telemetryQueryPeriodBasisRecord),
		Telemetry: *scan,
		Report:    state.BuildCallOutlierReport(scan.logs),
	})
}

func printCallOutliersHistory(st *state.StateStore, query TelemetryQueryArgs, stdout io.Writer) error {
	scan, err := st.ScanTelemetryHistory(query.Filter)
	if err != nil {
		return err
	}
	reports := make([]callOutliersHistoryReport, 0)
	for _, cohort := range scan.HistoryCohortLogs() {
		reports = append(reports, callOutliersHistoryReport{
			Version:        cohort.Version,
			SchemaRevision: cohort.SchemaRevision,
			Report:         state.BuildCallOutlierReport(cohort.Logs),
		})
	}
	return writeJSON(stdout, callOutliersHistoryOutput{
		Query:     query.view(telemetryQueryPeriodBasisRecord),
		Telemetry: *scan,
		Reports:   reports,
	})
}
