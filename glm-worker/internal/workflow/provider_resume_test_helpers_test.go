package workflow

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func canonicalProviderUnavailableCheckpoint(checkpoint state.ResumeCheckpoint) state.ResumeCheckpoint {
	checkpoint.ProviderUnavailableClassification = "http-503"
	checkpoint.ProviderUnavailableProbes = 4
	checkpoint.ProviderUnavailableStartedAt = time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC)
	return checkpoint
}
