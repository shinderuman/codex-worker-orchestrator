package app

import "strings"

const qualityGateActionSeparator = ":"

func init() {
	commandParsers["--quality-gate"] = qualityGateRecoveryCommand
}

func qualityGateRecoveryCommand(args []string) (Command, error) {
	if len(args) == 2 && qualityGateForms[args[1]] != nil {
		return Command{Mode: ModeQualityGate, Payload: args[1]}, nil
	}
	if len(args) == 3 {
		action := args[1]
		if (action == "status" || action == "watch" || action == "result" || action == "internal-run") && validValidationRunID(args[2]) {
			return Command{Mode: ModeQualityGate, Payload: action + qualityGateActionSeparator + args[2]}, nil
		}
	}
	return Command{}, usageError("usage: glm-worker --quality-gate <go-test|go-test-race> | --quality-gate <status|watch|result> <validation-run-id>")
}

func splitQualityGateAction(payload string) (string, string, bool) {
	action, runID, ok := strings.Cut(payload, qualityGateActionSeparator)
	if !ok || !validValidationRunID(runID) {
		return "", "", false
	}
	switch action {
	case "status", "watch", "result", "internal-run":
		return action, runID, true
	default:
		return "", "", false
	}
}
