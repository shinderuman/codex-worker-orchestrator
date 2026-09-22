package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func PrintParentReviewEvidence(cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	return parentevidence.PrintReviewEvidence(cfg.RepoRoot, st, stdout)
}

func buildParentReviewEvidenceManifest(st *state.StateStore) (parentEvidenceManifest, error) {
	return parentevidence.BuildReviewManifest(st)
}

func parentReviewEvidenceTarget(target string) (string, string, error) {
	return parentevidence.ReviewTarget(target)
}

func parentReviewNumericRange(locator string) (int, int, bool) {
	return parentevidence.NumericRange(locator)
}
