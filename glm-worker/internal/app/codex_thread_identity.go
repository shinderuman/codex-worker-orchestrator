package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const codexThreadIDEnv = "CODEX_THREAD_ID"

func bindCurrentCodexThreadIdentity(cmd *Command) error {
	if cmd.Mode != ModeVerifyAutoResume && cmd.Mode != ModeCheckWakeCoalesce && cmd.Mode != ModeAutoResumePlan {
		return nil
	}
	threadID := os.Getenv(codexThreadIDEnv)
	if !state.ValidUUIDFormat(threadID) {
		return &machinecli.NotFoundError{Message: codexThreadIDEnv + " is unavailable or invalid"}
	}
	switch cmd.Mode {
	case ModeVerifyAutoResume:
		cmd.Verify.ThreadID = threadID
	case ModeAutoResumePlan:
		cmd.AutoResume.ParentThreadID = threadID
	default:
		cmd.Coalesce.ParentThreadID = threadID
	}
	return nil
}
