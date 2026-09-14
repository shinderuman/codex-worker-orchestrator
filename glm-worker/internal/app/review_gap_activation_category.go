package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func reviewGapCategoryForPath(path string, repositoryHarnessActive *bool, activationErr error) (string, string, bool) {
	if !state.IsParentManagedPath(path) {
		return state.FixPathCategory(path), "", true
	}
	if repositoryHarnessActive == nil {
		if activationErr != nil {
			return "", reviewGapReasonRepositoryHarnessActivationUnreadable, false
		}
		return "", reviewGapReasonRepositoryHarnessActivationMissing, false
	}
	if *repositoryHarnessActive {
		return state.FixCategoryMetadata, "", true
	}
	return state.FixPathCategory(path), "", true
}
