package parentactioncmd

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func prepareCompletionVerification(cfg config.AppConfig, st *state.StateStore) (completionVerification, *repositoryproject.ParentRequestCompletionProjection) {
	if failure := verifyPublicationCompletionGate(cfg, st); failure != nil {
		return completionVerification{failure: failure}, nil
	}
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
	lifecycleTask := st.ReadOr("active-task", "")
	projection, err := repositoryprojecttree.BuildParentRequestCompletionProjection(cfg.RepoRoot, lifecycleTask)
	if err != nil {
		verification.failure = &finalizationFailure{
			Stage:  "project",
			Reason: completeFailureParentRequestProjectionError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
		return verification, nil
	}
	attribution, err := repositoryprojecttree.BuildTaskAttribution(cfg.RepoRoot, lifecycleTask, projection.Continuation)
	if err != nil {
		verification.failure = &finalizationFailure{
			Stage:  "project",
			Reason: completeFailureParentRequestProjectionError,
			Detail: compactFinalizationDiagnostic(err.Error()),
		}
		return verification, nil
	}
	if attribution.Handover {
		if err := verifyCompletionHandoverOwner(cfg.RepoRoot, st, lifecycleTask); err != nil {
			verification.failure = &finalizationFailure{
				Stage:  "metadata",
				Reason: "completion_transition_invalid",
				Detail: compactFinalizationDiagnostic(err.Error()),
			}
			return verification, nil
		}
	}
	if attribution.Reason == repositoryproject.ReasonActiveTaskMismatch && !attribution.Handover {
		verification.failure = &finalizationFailure{
			Stage:  "metadata",
			Reason: "completion_transition_invalid",
			Detail: compactFinalizationDiagnostic("lifecycle task " + attribution.LifecycleTask + " does not match current ACTIVE " + attribution.ActiveTask),
		}
		return verification, nil
	}
	verification.failure = verifyCompletionUnchanged(cfg.RepoRoot, verification.gitRepo, verification.verifiedHead)
	return verification, &projection
}

func verifyCompletionHandoverOwner(repoRoot string, st *state.StateStore, lifecycleTask string) error {
	if authorityTask, err := st.CurrentTaskAuthorityPath(); err == nil && authorityTask != lifecycleTask {
		return fmt.Errorf("lifecycle task %s does not match canonical task authority %s", lifecycleTask, authorityTask)
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return fmt.Errorf("publication candidate unavailable for lifecycle owner verification: %w", err)
	}
	baseEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", candidate.BaseHead, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from publication base: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(baseEntry) == "" {
		return fmt.Errorf("lifecycle task %s was not tracked at publication base", lifecycleTask)
	}
	candidateEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", candidate.CommitOID, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from publication candidate: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(candidateEntry) != "" {
		return fmt.Errorf("lifecycle task %s remains tracked in publication candidate", lifecycleTask)
	}
	return nil
}
