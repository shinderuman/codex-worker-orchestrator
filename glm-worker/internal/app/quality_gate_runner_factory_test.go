package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func defaultRunnerFactory(cfg config.AppConfig, st *state.StateStore, stop *runner.StopController) workflow.ModelRunner {
	r := runner.NewClaudeRunner(cfg, st)
	r.AttachStopController(stop)
	return r
}
