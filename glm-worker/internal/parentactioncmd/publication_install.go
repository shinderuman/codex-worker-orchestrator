package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationInstallOutput struct {
	Status       string               `json:"status"`
	Required     bool                 `json:"required"`
	CandidateOID string               `json:"candidate_oid,omitempty"`
	Failure      *finalizationFailure `json:"failure,omitempty"`
}

const (
	publicationInstallStatusInstalled   = "installed"
	publicationInstallStatusNotRequired = "not_required"
	publicationInstallStatusBlocked     = "blocked"
	publicationInstallStatusFailed      = "install_failed"
)

func runPublicationCandidateInstall(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "install-candidate" {
		return fmt.Errorf("usage: glm-parent-action push-binding install-candidate")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	return json.NewEncoder(stdout).Encode(installPublicationCandidate(cfg, st))
}

func installPublicationCandidate(cfg config.AppConfig, st *state.StateStore) publicationInstallOutput {
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return publicationInstallOutput{Status: publicationInstallStatusBlocked, Failure: publicationReadinessFailure(publicationFailureCandidateMissing, err.Error())}
	}
	output := publicationInstallOutput{Status: publicationInstallStatusBlocked, CandidateOID: candidate.CommitOID}
	if source := publicationSourceGate(cfg.RepoRoot, candidate); source.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureCandidateStale, source.Reason)
		return output
	}
	if review := publicationReviewGate(st, candidate); review.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureGateMissing, review.Reason)
		return output
	}
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureClassification, err.Error())
		return output
	}
	if !requirement.Required {
		output.Status = publicationInstallStatusNotRequired
		return output
	}
	output.Required = true
	if requirement.Head != candidate.BaseHead {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureStale, "runtime classification HEAD no longer matches publication candidate base")
		return output
	}
	requirement.Head = candidate.CommitOID
	if existing, failure := matchingPublicationRuntimeInstallEvidence(st, candidate, requirement); failure == nil {
		if _, _, probeFailure := installedRuntimeStatus(cfg, candidate.CommitOID); probeFailure == nil {
			output.Status = publicationInstallStatusInstalled
			return output
		}
		_ = existing
	}
	if err := st.ClearRuntimeInstallEvidence(); err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
		return output
	}
	worktree, failure := createPublicationInstallWorktree(cfg, candidate)
	if failure != nil {
		output.Failure = failure
		return output
	}
	script, failure := installScriptGuard(worktree)
	if failure == nil {
		attempt := runInstallScript(script, worktree, io.Discard)
		if attempt.Status != installStatusInstalled {
			failure = attempt.Failure
		}
	}
	if cleanupFailure := removePublicationInstallWorktree(cfg.RepoRoot, worktree); failure == nil && cleanupFailure != nil {
		failure = cleanupFailure
	}
	if failure != nil {
		output.Status = publicationInstallStatusFailed
		output.Failure = failure
		return output
	}
	if smokeFailure := runRuntimeInstallSmoke(cfg); smokeFailure != nil {
		output.Status = publicationInstallStatusFailed
		output.Failure = smokeFailure
		return output
	}
	if source := publicationSourceGate(cfg.RepoRoot, candidate); source.Status != publicationGatePass {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureStale, source.Reason)
		return output
	}
	if review := publicationReviewGate(st, candidate); review.Status != publicationGatePass {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureStale, review.Reason)
		return output
	}
	if err := verifyRuntimeMergedConfigFiles(cfg, requirement.Paths); err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
		return output
	}
	installedRevision, _, probeFailure := installedRuntimeStatus(cfg, candidate.CommitOID)
	if probeFailure != nil {
		output.Failure = probeFailure
		return output
	}
	taskID, err := st.TaskID()
	if err != nil || taskID != candidate.TaskID {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureStale, "task identity changed during publication candidate install")
		return output
	}
	if failure := recordRuntimeInstallValidation(st, taskID, requirement); failure != nil {
		output.Failure = failure
		return output
	}
	if err := st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              candidate.CommitOID,
		SourceDigest:      requirement.SourceDigest,
		InstalledRevision: installedRevision,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
		return output
	}
	output.Status = publicationInstallStatusInstalled
	return output
}

func matchingPublicationRuntimeInstallEvidence(st *state.StateStore, candidate state.PublicationCandidate, requirement runtimeInstallRequirement) (state.RuntimeInstallEvidence, *finalizationFailure) {
	evidence, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		return state.RuntimeInstallEvidence{}, runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	if evidence.TaskID != candidate.TaskID || evidence.Head != candidate.CommitOID || evidence.InstalledRevision != candidate.CommitOID ||
		evidence.SourceDigest != requirement.SourceDigest || evidence.SmokeResult != state.ValidationResultPass {
		return state.RuntimeInstallEvidence{}, runtimeInstallFailure(runtimeInstallFailureStale, "runtime install evidence does not match publication candidate")
	}
	return evidence, nil
}

func createPublicationInstallWorktree(cfg config.AppConfig, candidate state.PublicationCandidate) (string, *finalizationFailure) {
	if err := os.MkdirAll(cfg.WorktreeBase, 0o700); err != nil {
		return "", runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	path, err := os.MkdirTemp(cfg.WorktreeBase, "publication-candidate-")
	if err != nil {
		return "", runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	if _, err := gitFinalizationOutput(cfg.RepoRoot, "worktree", "add", "--detach", path, candidate.CommitOID); err != nil {
		_ = os.RemoveAll(path)
		return "", runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	return filepath.Clean(path), nil
}

func removePublicationInstallWorktree(repoRoot, worktree string) *finalizationFailure {
	if _, err := gitFinalizationOutput(repoRoot, "worktree", "remove", "--force", worktree); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, "candidate worktree cleanup failed: "+err.Error())
	}
	return nil
}
