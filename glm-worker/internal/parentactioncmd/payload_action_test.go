package parentactioncmd

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

const (
	actionFix              = string(parentaction.ActionFix)
	actionStartMilestones  = string(parentaction.ActionStartMilestones)
	actionReviseMilestones = string(parentaction.ActionReviseMilestones)
)

func payloadWorkerArgs(action string, payload []byte, options []string) []string {
	descriptor, ok := parentaction.LookupPayloadAction(action)
	if !ok {
		return nil
	}
	return payloadWorkerArgsForDescriptor(descriptor, payload, options)
}
