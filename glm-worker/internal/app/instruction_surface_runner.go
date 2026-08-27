package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func instructionSurfaceRunnerFactory(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner {
	base := runner.NewClaudeRunner(cfg, st)
	base.AttachStopController(stop)
	return runner.NewInstructionSurfaceGuardRunner(base)
}
