package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"

const handoffTerminalView = "terminal"

func parentHandoffCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeHandoff}, nil
	}
	if len(args) == 2 && (args[1] == "recovery" || args[1] == handoffTerminalView) {
		return Command{Mode: ModeHandoff, Payload: args[1]}, nil
	}
	return Command{}, machinecli.UsageErrorf("usage: glm-worker --handoff [recovery|terminal]")
}
