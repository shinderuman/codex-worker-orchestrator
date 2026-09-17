package parentactioncmd

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareCompletionVerification(cfg config.AppConfig, st *state.StateStore) (completionVerification, *repositoryproject.ParentRequestCompletionProjection) {
	verification := verifyParentCompletion(cfg.RepoRoot, st)
	if verification.failure != nil {
		return verification, nil
	}
	verification.failure = verifyRuntimeInstallCompletion(cfg, st)
	if verification.failure != nil {
		return verification, nil
	}
	if !verification.repositoryHarnessActive {
		verification.failure = verifyCompletionUnchanged(cfg.RepoRoot, verification.gitRepo, verification.verifiedHead)
		return verification, nil
	}
	projection, err := repositoryprojecttree.BuildParentRequestCompletionProjection(cfg.RepoRoot, st.ReadOr("active-task", ""))
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
