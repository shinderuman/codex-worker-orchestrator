package parentactioncmd

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	pushBindingAuthorizationPublication = "publication_candidate_ready"
	publicationFailureRemoteNotReady    = "publication_remote_write_not_ready"
)

func applyPublicationRemoteWriteGuard(repoRoot string, output pushBindingOutput) pushBindingOutput {
	if output.RemoteWrite == nil {
		return output
	}
	decision, err := repositoryharness.Evaluate(repoRoot)
	if err != nil {
		return blockPublicationRemoteWrite(output, err.Error())
	}
	if !decision.Active {
		return output
	}
	cfg, err := config.Load()
	if err != nil || cfg.RepoRoot != repoRoot {
		return blockPublicationRemoteWrite(output, "publication repository identity is unavailable")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return blockPublicationRemoteWrite(output, err.Error())
	}
	if st.TaskStatus() == state.TaskStatusNone {
		return output
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return blockPublicationRemoteWrite(output, "publication candidate is missing: "+err.Error())
	}
	if output.Target == nil || output.Target.LocalOID != candidate.CommitOID || output.ExpectedOID != candidate.CommitOID || !output.TreeClean {
		return blockPublicationRemoteWrite(output, "local publication target is not the promoted candidate")
	}
	if failure := verifyPromotedPublicationReadiness(cfg, st, candidate); failure != nil {
		return blockPublicationRemoteWrite(output, failure.Detail)
	}
	output.RemoteWrite.Authorization = pushBindingAuthorizationPublication
	return output
}

func verifyPromotedPublicationReadiness(cfg config.AppConfig, st *state.StateStore, candidate state.PublicationCandidate) *finalizationFailure {
	if failure := verifyPublicationCandidateCommit(cfg.RepoRoot, candidate); failure != nil {
		return failure
	}
	if source := publicationSourceGate(cfg.RepoRoot, candidate); source.Status != publicationGatePass {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, source.Reason)
	}
	if review := publicationReviewGate(st, candidate); review.Status != publicationGatePass {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, review.Reason)
	}
	if validation, required := publicationParentValidationGate(st, cfg.RepoRoot, candidate); required && validation.Status != publicationGatePass {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, validation.Gate+": "+validation.Reason)
	}
	return verifyPromotedRuntimeInstallReadiness(cfg, st, candidate)
}

func verifyPromotedRuntimeInstallReadiness(cfg config.AppConfig, st *state.StateStore, candidate state.PublicationCandidate) *finalizationFailure {
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, err.Error())
	}
	if !requirement.Required {
		return nil
	}
	if requirement.Head != candidate.CommitOID {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, "runtime classification HEAD is not the promoted candidate")
	}
	if _, failure := matchingPublicationRuntimeInstallEvidence(st, candidate, requirement); failure != nil {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, failure.Detail)
	}
	if _, _, failure := installedRuntimeStatus(cfg, candidate.CommitOID); failure != nil {
		return publicationReadinessFailure(publicationFailureRemoteNotReady, failure.Detail)
	}
	return nil
}

func blockPublicationRemoteWrite(output pushBindingOutput, detail string) pushBindingOutput {
	output.Status = "blocked"
	output.RemoteWrite = nil
	output.Failure = publicationReadinessFailure(publicationFailureRemoteNotReady, compactFinalizationDiagnostic(fmt.Sprint(detail)))
	return output
}
