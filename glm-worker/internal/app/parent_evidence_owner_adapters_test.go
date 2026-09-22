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

func parentReviewEvidenceClaims(targets []string, parts []parentEvidencePart) ([]state.ParentReviewEvidenceClaim, bool) {
	return parentevidence.ReviewClaims(targets, parts)
}

func parentReviewDiffCoversTarget(target string, diff parentEvidenceDiffBody) bool {
	return parentevidence.ReviewDiffCoversTarget(target, diff)
}

func parentReviewDiffFileSection(body, path string) string {
	return parentevidence.ReviewDiffFileSection(body, path)
}

func saveParentEvidenceLedger(st *state.StateStore, surface, digest, origin, ownerCallID string) error {
	return parentevidence.SaveLedger(st, surface, digest, origin, ownerCallID)
}
