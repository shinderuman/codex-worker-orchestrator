package app

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const codexThreadIDEnv = "CODEX_THREAD_ID"

func bindCurrentCodexThreadIdentity(cmd *Command) error {
	if cmd.Mode != ModeVerifyAutoResume && cmd.Mode != ModeCheckWakeCoalesce {
		return nil
	}
	threadID := os.Getenv(codexThreadIDEnv)
	if !state.ValidUUIDFormat(threadID) {
		return &NotFoundError{Message: codexThreadIDEnv + " is unavailable or invalid"}
	}
	if cmd.Mode == ModeVerifyAutoResume {
		cmd.Verify.ThreadID = threadID
	} else {
		cmd.Coalesce.ParentThreadID = threadID
	}
	return nil
}
