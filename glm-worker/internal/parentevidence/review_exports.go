package parentevidence

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func ReviewClaims(targets []string, parts []Part) ([]state.ParentReviewEvidenceClaim, bool) {
	return reviewClaims(targets, parts)
}

func ReviewDiffCoversTarget(target string, diff DiffBody) bool {
	return reviewDiffCoversTarget(target, diff)
}
