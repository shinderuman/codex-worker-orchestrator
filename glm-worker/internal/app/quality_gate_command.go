package app

import "strings"

const (
	qualityGateActionSeparator = ":"
	qualityGateActionStatus    = "status"
	qualityGateActionWatch     = "watch"
	qualityGateActionResult    = "result"
	qualityGateActionInternal  = "internal-run"
	qualityGateCommandUsage    = "usage: glm-worker --quality-gate <go-test|go-test-race> | --quality-gate <status|watch|result> <validation-run-id>"
)

func qualityGateRecoveryCommand(args []string) (Command, error) {
	if len(args) == 2 && qualityGateForms[args[1]] != nil {
		return Command{Mode: ModeQualityGate, Payload: args[1]}, nil
	}
	if len(args) == 3 && validQualityGateAction(args[1]) && validValidationRunID(args[2]) {
		return Command{Mode: ModeQualityGate, Payload: args[1] + qualityGateActionSeparator + args[2]}, nil
	}
	return Command{}, usageError("%s", qualityGateCommandUsage)
}

func validQualityGateAction(action string) bool {
	switch action {
	case qualityGateActionStatus, qualityGateActionWatch, qualityGateActionResult, qualityGateActionInternal:
		return true
	default:
		return false
	}
}

func splitQualityGateAction(payload string) (string, string, bool) {
	action, runID, ok := strings.Cut(payload, qualityGateActionSeparator)
	if !ok || !validQualityGateAction(action) || !validValidationRunID(runID) {
		return "", "", false
	}
	return action, runID, true
}
