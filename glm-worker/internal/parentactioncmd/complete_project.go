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
	authorityTask, err := st.CurrentTaskAuthorityPath()
	if err != nil {
		return fmt.Errorf("canonical task authority unavailable for lifecycle owner verification: %w", err)
	}
	if authorityTask != lifecycleTask {
		return fmt.Errorf("lifecycle task %s does not match canonical task authority %s", lifecycleTask, authorityTask)
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return fmt.Errorf("publication candidate unavailable for lifecycle owner verification: %w", err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		return fmt.Errorf("current task identity unavailable for lifecycle owner verification: %w", err)
	}
	if candidate.TaskID != taskID {
		return fmt.Errorf("publication candidate task identity %s does not match current task identity %s", candidate.TaskID, taskID)
	}
	baseEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", candidate.BaseHead, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from publication base: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(baseEntry) == "" {
		if err := verifyCompletionReopenLineage(repoRoot, st, lifecycleTask, taskID, candidate); err != nil {
			return err
		}
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

func verifyCompletionReopenLineage(repoRoot string, st *state.StateStore, lifecycleTask, taskID string, candidate state.PublicationCandidate) error {
	lineage, err := st.LoadPublicationReopenLineage()
	if err != nil {
		return fmt.Errorf("lifecycle task %s was not tracked at publication base and reopen lineage is unavailable: %w", lifecycleTask, err)
	}
	if err := verifyCompletionReopenLineageIdentity(lineage, lifecycleTask, taskID); err != nil {
		return err
	}
	if err := verifyCompletionReopenLineageTransition(repoRoot, lineage, lifecycleTask); err != nil {
		return err
	}
	if _, err := gitFinalizationOutput(repoRoot, "merge-base", "--is-ancestor", lineage.CommitOID, candidate.BaseHead); err != nil {
		return fmt.Errorf("publication candidate base %s does not descend from reopen lineage commit %s", candidate.BaseHead, lineage.CommitOID)
	}
	return nil
}

func verifyCompletionReopenLineageIdentity(lineage state.PublicationReopenLineage, lifecycleTask, taskID string) error {
	if lineage.TaskID != taskID {
		return fmt.Errorf("publication reopen lineage task identity %s does not match current task identity %s", lineage.TaskID, taskID)
	}
	if lineage.TaskPath != lifecycleTask {
		return fmt.Errorf("publication reopen lineage task path %s does not match lifecycle task %s", lineage.TaskPath, lifecycleTask)
	}
	return nil
}

func verifyCompletionReopenLineageTransition(repoRoot string, lineage state.PublicationReopenLineage, lifecycleTask string) error {
	parents, err := gitFinalizationOutput(repoRoot, "rev-list", "--parents", "-n", "1", lineage.CommitOID)
	if err != nil {
		return fmt.Errorf("publication reopen lineage commit %s cannot be read: %w", lineage.CommitOID, err)
	}
	parentFields := strings.Fields(parents)
	if len(parentFields) != 2 || parentFields[0] != lineage.CommitOID || parentFields[1] != lineage.BaseHead {
		return fmt.Errorf("publication reopen lineage commit %s is not directly based on %s", lineage.CommitOID, lineage.BaseHead)
	}
	lineageBaseEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", lineage.BaseHead, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from reopen lineage base: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(lineageBaseEntry) == "" {
		return fmt.Errorf("lifecycle task %s was not tracked at reopen lineage base", lifecycleTask)
	}
	lineageCommitEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", lineage.CommitOID, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from reopen lineage commit: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(lineageCommitEntry) != "" {
		return fmt.Errorf("lifecycle task %s remains tracked in reopen lineage commit", lifecycleTask)
	}
	return nil
}
