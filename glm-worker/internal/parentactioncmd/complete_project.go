package parentactioncmd

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareCompletionVerification(cfg config.AppConfig, st *state.StateStore) (completionVerification, *app.ParentRequestCompletionProjection) {
	verification := verifyParentCompletion(cfg.RepoRoot, st)
	if verification.failure != nil {
		return verification, nil
	}
	verification.failure = verifyRuntimeInstallCompletion(cfg, st)
	if verification.failure != nil {
		return verification, nil
	}
	projection, err := app.BuildParentRequestCompletionProjection(cfg)
	if err != nil {
		verification.failure = &finalizationFailure{
			Stage:  "project",
			Reason: completeFailureParentRequestProjectionError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
		return verification, nil
	}
	verification.failure = verifyCompletionUnchanged(cfg.RepoRoot, verification.gitRepo, verification.verifiedHead)
	return verification, &projection
}
