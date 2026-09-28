package runner

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (r *ClaudeRunner) runWithAdmission(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
	admit func() error,
) (RunResult, error) {
	return r.runSession(role, phase, model, readOnly, effort, prompt, outputPath, time.Time{}, admit)
}
