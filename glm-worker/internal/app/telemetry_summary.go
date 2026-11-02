package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/bundleanalysis"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func printTelemetryCompactSummary(cfg config.AppConfig, st *state.StateStore, query report.Query, stdout io.Writer) error {
	summary, err := bundleanalysis.BuildTelemetryCompactSummary(cfg, st, query)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, summary)
}
