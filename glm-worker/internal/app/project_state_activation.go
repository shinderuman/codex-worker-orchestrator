package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func executeProjectStateInspection(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	active, err := workflow.RepositoryHarnessActive(cfg.RepoRoot, st)
	if err != nil {
		return err
	}
	if active {
		return executeStatelessProjection(cmd, cfg, stdout)
	}
	return writeJSON(stdout, projectStateOutput{
		Version:      projectStateVersion,
		Dependencies: []projectStateDependency{},
		Blockers:     []projectStateBlocker{},
		Continuation: unknownProjectContinuation(projectContinuationReasonPlanAbsent),
	})
}
