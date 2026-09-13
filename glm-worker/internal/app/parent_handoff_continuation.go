package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func applyParentRequestCompletion(repoRoot string, st *state.StateStore, output *parentHandoffOutput) {
	if repoRoot == "" {
		return
	}
	active, err := workflow.RepositoryHarnessActive(repoRoot, st)
	if err != nil {
		markHandoffInconsistent(output, "repository harness activation is unavailable: "+err.Error())
		return
	}
	if !active {
		return
	}
	projection, err := BuildCurrentParentRequestCompletionProjection(config.AppConfig{RepoRoot: repoRoot}, st)
	if err != nil {
		markHandoffInconsistent(output, "project continuation projection is unavailable: "+err.Error())
		return
	}
	output.ParentRequest = &projection
}
