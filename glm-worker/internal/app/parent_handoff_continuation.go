package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func applyParentRequestCompletion(repoRoot string, st *state.StateStore, output *parentHandoffOutput) {
	if repoRoot == "" {
		return
	}
	projection, err := BuildCurrentParentRequestCompletionProjection(config.AppConfig{RepoRoot: repoRoot}, st)
	if err != nil {
		markHandoffInconsistent(output, "project continuation projection is unavailable: "+err.Error())
		return
	}
	output.ParentRequest = &projection
}
