package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"

const shadowEvalUsage = "usage: glm-worker --shadow-eval <task-id> [--reference <reference.json>]"

const failurePathAdvisoryUsage = "usage: glm-worker --failure-path-advisory [--labels <labels.json>]"

func shadowEvalCommand(args []string) (Command, error) {
	if len(args) < 2 || len(args)%2 != 0 {
		return Command{}, machinecli.UsageErrorf("%s", shadowEvalUsage)
	}
	command := Command{Mode: ModeShadowEval, Payload: args[1]}
	for index := 2; index < len(args); index += 2 {
		if args[index] != "--reference" || args[index+1] == "" || command.ReferencePath != "" {
			return Command{}, machinecli.UsageErrorf("%s", shadowEvalUsage)
		}
		command.ReferencePath = args[index+1]
	}
	return command, nil
}

func failurePathAdvisoryCommand(args []string) (Command, error) {
	if len(args) == 1 {
		return Command{Mode: ModeFailurePathAdvisory}, nil
	}
	if len(args) == 3 && args[1] == "--labels" && args[2] != "" {
		return Command{Mode: ModeFailurePathAdvisory, ReferencePath: args[2]}, nil
	}
	return Command{}, machinecli.UsageErrorf("%s", failurePathAdvisoryUsage)
}
