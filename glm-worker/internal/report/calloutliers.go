package report

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type callOutliersOutput struct {
	Query     queryView               `json:"query"`
	Telemetry TelemetryScan           `json:"telemetry"`
	Report    state.CallOutlierReport `json:"report"`
}

type callOutliersHistoryReport struct {
	Version        int                     `json:"version"`
	SchemaRevision int                     `json:"schema_revision"`
	Report         state.CallOutlierReport `json:"report"`
}

type callOutliersHistoryOutput struct {
	Query     queryView                   `json:"query"`
	Telemetry state.TelemetryHistoryScan  `json:"telemetry"`
	Reports   []callOutliersHistoryReport `json:"reports"`
}

func PrintCallOutliers(cfg config.AppConfig, st *state.StateStore, query Query, compact CompactSummaryFunc, stdout io.Writer) error {
	if query.Compact {
		return compact(cfg, st, query, stdout)
	}
	if query.IsHistory() {
		return printCallOutliersHistory(st, query, stdout)
	}
	scan, err := ScanTelemetryTaskLogs(st, query.Filter)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, callOutliersOutput{
		Query:     query.view(QueryPeriodBasisRecord),
		Telemetry: *scan,
		Report:    state.BuildCallOutlierReport(scan.Logs),
	})
}

func printCallOutliersHistory(st *state.StateStore, query Query, stdout io.Writer) error {
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
	return machinecli.WriteJSON(stdout, callOutliersHistoryOutput{
		Query:     query.view(QueryPeriodBasisRecord),
		Telemetry: *scan,
		Reports:   reports,
	})
}
