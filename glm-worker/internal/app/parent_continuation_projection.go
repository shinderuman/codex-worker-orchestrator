package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentContinuationProjection struct {
	Consistent    bool
	Inconsistency *string
	ParentRequest *ParentRequestCompletionProjection
}

func BuildParentContinuationProjection(cfg config.AppConfig) ParentContinuationProjection {
	output := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	return ParentContinuationProjection{
		Consistent:    output.Consistent,
		Inconsistency: output.Inconsistency,
		ParentRequest: output.ParentRequest,
	}
}
