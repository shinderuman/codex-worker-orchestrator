package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"

func parentHandoffCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeHandoff}, nil
	}
	if len(args) == 2 && args[1] == "recovery" {
		return Command{Mode: ModeHandoff, Payload: "recovery"}, nil
	}
	return Command{}, machinecli.UsageErrorf("usage: glm-worker --handoff [recovery]")
}
