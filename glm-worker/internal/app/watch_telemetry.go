package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

const modelCallOutcomeError = "error"

func lastParentActionMaterial(logs []state.ModelCallLog) *state.ModelCallLog {
	for index := len(logs) - 1; index >= 0; index-- {
		if logs[index].CallType == state.CallTypeProbe {
			continue
		}
		return &logs[index]
	}
	return nil
}
