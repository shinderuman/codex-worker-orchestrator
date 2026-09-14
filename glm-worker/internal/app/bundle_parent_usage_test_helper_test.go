package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func buildParentUsageReport(cfg config.AppConfig, st *state.StateStore, task bundleTask) parentUsageReport {
	start, collectionEnd, _ := analysisCollectionWindow(task)
	association := resolveCodexAssociation(cfg.CodexConfigDir, task)
	scan, scanErr := parentUsageRolloutScan(association, start, collectionEnd)
	return buildParentUsageReportFromScan(st, task, association, scan, scanErr)
}
