package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const verifyCodexWakeUsage = "usage: glm-worker --verify-codex-wake <wake-task-thread-id> <wake-at-rfc3339>"

func verifyCodexWakeCommand(args []string) (Command, error) {
	if len(args) != 3 || !state.ValidUUIDFormat(args[1]) {
		return Command{}, machinecli.UsageErrorf("%s", verifyCodexWakeUsage)
	}
	return Command{
		Mode: ModeVerifyCodexWake,
		Verify: VerifyArgs{
			ThreadID: args[1],
			RFC3339:  args[2],
		},
	}, nil
}
