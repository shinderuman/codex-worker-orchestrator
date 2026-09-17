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

type publicationInstallContext struct {
	candidate   state.PublicationCandidate
	requirement runtimeInstallRequirement
}

const (
	publicationInstallStatusInstalled   = "installed"
	publicationInstallStatusNotRequired = "not_required"
	publicationInstallStatusBlocked     = "blocked"
	publicationInstallStatusFailed      = "install_failed"
)

func runPublicationCandidateInstall(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != publicationInstallCandidateSubcommand {
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
	context, terminal := publicationInstallContextFor(cfg, st)
	if terminal != nil {
		return *terminal
	}
	output := publicationInstallOutput{
		Status:       publicationInstallStatusBlocked,
		Required:     true,
		CandidateOID: context.candidate.CommitOID,
	}
	if publicationCandidateAlreadyInstalled(cfg, st, context) {
		output.Status = publicationInstallStatusInstalled
		return output
	}
	if err := st.ClearRuntimeInstallEvidence(); err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
		return output
	}
	if failure := executePublicationCandidateInstall(cfg, context.candidate); failure != nil {
		output.Status = publicationInstallStatusFailed
		output.Failure = failure
		return output
	}
	if failure := persistPublicationCandidateInstall(cfg, st, context); failure != nil {
		output.Failure = failure
		return output
	}
	output.Status = publicationInstallStatusInstalled
	return output
}

func publicationInstallContextFor(cfg config.AppConfig, st *state.StateStore) (publicationInstallContext, *publicationInstallOutput) {
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return publicationInstallContext{}, &publicationInstallOutput{
			Status:  publicationInstallStatusBlocked,
			Failure: publicationReadinessFailure(publicationFailureCandidateMissing, err.Error()),
		}
	}
	output := publicationInstallOutput{Status: publicationInstallStatusBlocked, CandidateOID: candidate.CommitOID}
	if source := publicationSourceGate(cfg.RepoRoot, candidate); source.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureCandidateStale, source.Reason)
		return publicationInstallContext{}, &output
	}
	if review := publicationReviewGate(st, candidate); review.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureGateMissing, review.Reason)
		return publicationInstallContext{}, &output
	}
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureClassification, err.Error())
		return publicationInstallContext{}, &output
	}
	if !requirement.Required {
		output.Status = publicationInstallStatusNotRequired
		return publicationInstallContext{}, &output
	}
	output.Required = true
	if requirement.Head != candidate.BaseHead {
		output.Failure = runtimeInstallFailure(runtimeInstallFailureStale, "runtime classification HEAD no longer matches publication candidate base")
		return publicationInstallContext{}, &output
	}
	requirement.Head = candidate.CommitOID
	return publicationInstallContext{candidate: candidate, requirement: requirement}, nil
}

func publicationCandidateAlreadyInstalled(cfg config.AppConfig, st *state.StateStore, context publicationInstallContext) bool {
	if _, failure := matchingPublicationRuntimeInstallEvidence(st, context.candidate, context.requirement); failure != nil {
		return false
	}
	_, _, failure := installedRuntimeStatus(cfg, context.candidate.CommitOID)
	return failure == nil
}

func executePublicationCandidateInstall(cfg config.AppConfig, candidate state.PublicationCandidate) *finalizationFailure {
	worktree, failure := createPublicationInstallWorktree(cfg, candidate)
	if failure != nil {
		return failure
	}
	failure = runPublicationInstallScript(worktree)
	if cleanupFailure := removePublicationInstallWorktree(cfg.RepoRoot, worktree); failure == nil {
		failure = cleanupFailure
	}
	if failure != nil {
		return failure
	}
	return runRuntimeInstallSmoke(cfg)
}

func runPublicationInstallScript(worktree string) *finalizationFailure {
	script, failure := installScriptGuard(worktree)
	if failure != nil {
		return failure
	}
	attempt := runInstallScript(script, worktree, io.Discard)
	if attempt.Status != installStatusInstalled {
		return attempt.Failure
	}
	return nil
}

func persistPublicationCandidateInstall(cfg config.AppConfig, st *state.StateStore, context publicationInstallContext) *finalizationFailure {
	candidate := context.candidate
	requirement := context.requirement
	if source := publicationSourceGate(cfg.RepoRoot, candidate); source.Status != publicationGatePass {
		return runtimeInstallFailure(runtimeInstallFailureStale, source.Reason)
	}
	if review := publicationReviewGate(st, candidate); review.Status != publicationGatePass {
		return runtimeInstallFailure(runtimeInstallFailureStale, review.Reason)
	}
	if err := verifyRuntimeMergedConfigFiles(cfg, requirement.Paths); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureInstalled, err.Error())
	}
	installedRevision, _, probeFailure := installedRuntimeStatus(cfg, candidate.CommitOID)
	if probeFailure != nil {
		return probeFailure
	}
	taskID, err := st.TaskID()
	if err != nil || taskID != candidate.TaskID {
		return runtimeInstallFailure(runtimeInstallFailureStale, "task identity changed during publication candidate install")
	}
	if failure := recordRuntimeInstallValidation(st, taskID, requirement); failure != nil {
		return failure
	}
	if err := st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              candidate.CommitOID,
		SourceDigest:      requirement.SourceDigest,
		InstalledRevision: installedRevision,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		return runtimeInstallFailure(runtimeInstallFailureEvidence, err.Error())
	}
	return nil
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
