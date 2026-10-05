package app

import (
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
)

const repoSearchUsage = "usage: glm-worker --repo-search <question> --scope <path|symbol:<identifier>> [--scope ...] --budget <bytes>"

const repoSearchMaxBudgetBytes = 64 * 1024

func repoSearchCommand(args []string) (Command, error) {
	if len(args) < 2 || args[1] == "" || len(args[2:])%2 != 0 {
		return Command{}, machinecli.UsageErrorf("%s", repoSearchUsage)
	}
	command := Command{Mode: ModeRepoSearch, Payload: args[1]}
	seenBudget := false
	for index := 2; index < len(args); index += 2 {
		budgetSeen, err := applyRepoSearchOption(&command, args[index], args[index+1])
		if err != nil {
			return Command{}, err
		}
		seenBudget = seenBudget || budgetSeen
	}
	if len(command.SearchScopes) == 0 || !seenBudget {
		return Command{}, machinecli.UsageErrorf("%s", repoSearchUsage)
	}
	return command, nil
}

func applyRepoSearchOption(command *Command, name string, value string) (bool, error) {
	switch name {
	case "--scope":
		if value == "" {
			return false, machinecli.UsageErrorf("%s", repoSearchUsage)
		}
		command.SearchScopes = append(command.SearchScopes, value)
		return false, nil
	case "--budget":
		budget, err := strconv.Atoi(value)
		if err != nil || budget <= 0 || budget > repoSearchMaxBudgetBytes {
			return false, machinecli.UsageErrorf("%s", repoSearchUsage)
		}
		if command.SearchBudgetBytes != 0 {
			return false, machinecli.UsageErrorf("%s", repoSearchUsage)
		}
		command.SearchBudgetBytes = budget
		return true, nil
	default:
		return false, machinecli.UsageErrorf("%s", repoSearchUsage)
	}
}
