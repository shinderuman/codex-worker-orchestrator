package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type bundleTask struct {
	ID      string
	Status  string
	Current bool
	Stats   state.TaskStats
}
