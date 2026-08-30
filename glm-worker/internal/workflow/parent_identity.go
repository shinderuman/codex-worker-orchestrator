package workflow

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) persistParentActionCodexIdentity() error {
	threadID := os.Getenv(state.ParentActionCodexThreadIDEnv)
	sessionID := os.Getenv(state.ParentActionCodexSessionIDEnv)
	if !state.ValidUUIDFormat(threadID) || !state.ValidUUIDFormat(sessionID) {
		return nil
	}
	return w.state.SetParentCodexIdentity(threadID, sessionID)
}
