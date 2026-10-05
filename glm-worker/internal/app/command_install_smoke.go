package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"

const installSmokeUsage = "[--role worker|reviewer|fix|parent]"

func installSmokeCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeInstallSmoke}, nil
	}
	if len(args) == 3 && args[1] == "--role" && validInstallSmokeRoles[args[2]] {
		return Command{Mode: ModeInstallSmoke, Role: args[2]}, nil
	}
	return Command{}, machinecli.UsageErrorf("usage: glm-worker --install-smoke %s", installSmokeUsage)
}
