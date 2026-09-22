package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// ParentContinuationProjection is the typed in-process subset of the canonical
// parent handoff consumed by continuation admission gates.
type ParentContinuationProjection struct {
	Consistent    bool
	Inconsistency *string
	ParentRequest *ParentRequestCompletionProjection
}

// BuildParentContinuationProjection derives the same continuation/completion
// facts used by the canonical handoff without routing them through the CLI JSON
// transport or parent-evidence delivery ledger.
func BuildParentContinuationProjection(cfg config.AppConfig) ParentContinuationProjection {
	output := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	return ParentContinuationProjection{
		Consistent:    output.Consistent,
		Inconsistency: output.Inconsistency,
		ParentRequest: output.ParentRequest,
	}
}
