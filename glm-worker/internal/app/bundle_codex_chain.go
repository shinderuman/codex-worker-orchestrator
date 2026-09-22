package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"

func resolveCodexRolloutChain(matches []codexRollout) ([]codexRollout, string) {
	return codexrollout.ResolveChain(matches)
}
