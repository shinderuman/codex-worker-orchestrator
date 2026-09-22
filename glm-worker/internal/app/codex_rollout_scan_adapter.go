package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"

func scanCodexRollouts(codexHome string) ([]codexRollout, error) {
	return codexrollout.Scan(codexHome)
}
