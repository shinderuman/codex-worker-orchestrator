package parentactioncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationPromotionOutput struct {
	Status       string               `json:"status"`
	CandidateOID string               `json:"candidate_oid,omitempty"`
	BranchRef    string               `json:"branch_ref,omitempty"`
	Failure      *finalizationFailure `json:"failure,omitempty"`
}

const (
	publicationPromotionStatusPromoted = "promoted"
	publicationPromotionStatusBlocked  = "blocked"

	publicationFailurePromotionHead     = "publication_promotion_head_mismatch"
	publicationFailurePromotionRef      = "publication_promotion_ref_update_failed"
	publicationFailurePromotionRollback = "publication_promotion_rollback_failed"
)

func runPublicationPromotion(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != publicationPromoteSubcommand {
		return fmt.Errorf("usage: glm-parent-action push-binding promote")
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
	return json.NewEncoder(stdout).Encode(promotePublicationCandidate(cfg, st))
}

func promotePublicationCandidate(cfg config.AppConfig, st *state.StateStore) publicationPromotionOutput {
	readiness := projectPublicationReadiness(cfg, st)
	if readiness.Status != publicationReadinessReady {
		return publicationPromotionOutput{
			Status:       publicationPromotionStatusBlocked,
			CandidateOID: readiness.CandidateOID,
			Failure:      readiness.Failure,
		}
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return blockedPublicationPromotion(readiness.CandidateOID, publicationFailureCandidateMissing, err.Error())
	}
	return promoteReadyPublicationCandidate(cfg, candidate)
}

func promoteReadyPublicationCandidate(cfg config.AppConfig, candidate state.PublicationCandidate) publicationPromotionOutput {
	branchRef, headOID, failure := publicationPromotionHead(cfg.RepoRoot)
	if failure != nil {
		return publicationPromotionOutput{Status: publicationPromotionStatusBlocked, CandidateOID: candidate.CommitOID, Failure: failure}
	}
	if headOID == candidate.CommitOID {
		if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure != nil {
			return rollbackPublicationPromotionControlled(cfg, candidate, branchRef, failure)
		}
		return publicationPromotionOutput{Status: publicationPromotionStatusPromoted, CandidateOID: candidate.CommitOID, BranchRef: branchRef}
	}
	if headOID != candidate.BaseHead {
		return blockedPublicationPromotion(candidate.CommitOID, publicationFailurePromotionHead, "current HEAD does not match publication candidate base")
	}
	if failure := verifyPublicationCandidateCommit(cfg.RepoRoot, candidate); failure != nil {
		return publicationPromotionOutput{Status: publicationPromotionStatusBlocked, CandidateOID: candidate.CommitOID, BranchRef: branchRef, Failure: failure}
	}
	if err := updatePublicationRefControlled(cfg, candidate, branchRef, candidate.CommitOID, candidate.BaseHead); err != nil {
		return blockedPublicationPromotion(candidate.CommitOID, publicationFailurePromotionRef, err.Error())
	}
	if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure != nil {
		return rollbackPublicationPromotionControlled(cfg, candidate, branchRef, failure)
	}
	return publicationPromotionOutput{Status: publicationPromotionStatusPromoted, CandidateOID: candidate.CommitOID, BranchRef: branchRef}
}

func publicationPromotionPostcondition(repoRoot string, candidate state.PublicationCandidate) *finalizationFailure {
	source := publicationSourceGate(repoRoot, candidate)
	if source.Status == publicationGatePass && pushBindingTreeClean(repoRoot) {
		return nil
	}
	detail := source.Reason
	if detail == "" {
		detail = "promoted candidate source is not clean"
	}
	return publicationReadinessFailure(publicationFailurePromotionHead, detail)
}

func rollbackPublicationPromotionControlled(cfg config.AppConfig, candidate state.PublicationCandidate, branchRef string, cause *finalizationFailure) publicationPromotionOutput {
	if err := updatePublicationRefControlled(cfg, candidate, branchRef, candidate.BaseHead, candidate.CommitOID); err != nil {
		detail := publicationFailureDetail(cause) + "; rollback failed: " + err.Error()
		return publicationPromotionOutput{
			Status:       publicationPromotionStatusBlocked,
			CandidateOID: candidate.CommitOID,
			BranchRef:    branchRef,
			Failure:      publicationReadinessFailure(publicationFailurePromotionRollback, detail),
		}
	}
	return publicationPromotionOutput{
		Status:       publicationPromotionStatusBlocked,
		CandidateOID: candidate.CommitOID,
		BranchRef:    branchRef,
		Failure:      cause,
	}
}

func rollbackPublicationPromotion(repoRoot string, candidate state.PublicationCandidate, branchRef string, cause *finalizationFailure) publicationPromotionOutput {
	if err := updatePublicationRef(repoRoot, candidate, branchRef, candidate.BaseHead, candidate.CommitOID); err != nil {
		detail := publicationFailureDetail(cause) + "; rollback failed: " + err.Error()
		return publicationPromotionOutput{
			Status:       publicationPromotionStatusBlocked,
			CandidateOID: candidate.CommitOID,
			BranchRef:    branchRef,
			Failure:      publicationReadinessFailure(publicationFailurePromotionRollback, detail),
		}
	}
	return publicationPromotionOutput{
		Status:       publicationPromotionStatusBlocked,
		CandidateOID: candidate.CommitOID,
		BranchRef:    branchRef,
		Failure:      cause,
	}
}

func updatePublicationRefControlled(cfg config.AppConfig, candidate state.PublicationCandidate, branchRef, newOID, oldOID string) error {
	st := state.AttachStateStore(cfg)
	active, err := repositoryharness.RuntimeActive(cfg.RepoRoot, st)
	if err != nil {
		return err
	}
	if !active {
		return updatePublicationRef(cfg.RepoRoot, candidate, branchRef, newOID, oldOID)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	workspace, err := controller.ResolveWorkspaceIdentity(cfg.RepoRoot, store.Identity())
	if err != nil {
		return err
	}
	before, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		return err
	}
	task, err := controller.ResolveSemanticTaskRef(cfg.RepoRoot, st.ReadOr("active-task", ""))
	if err != nil {
		return err
	}
	admission, err := store.AdmitMutation(task, workspace, before)
	if err != nil {
		return err
	}
	effect := controller.EffectExpectation{
		Surface:     controller.MutationSurfaceRef,
		Resource:    branchRef,
		ExpectedOld: oldOID,
		ExpectedNew: newOID,
	}
	record, err := store.BeginTransition("publication-ref-update", admission.Head.ControllerGeneration, []controller.EffectExpectation{effect})
	if err != nil {
		return err
	}
	applyErr := updatePublicationRef(cfg.RepoRoot, candidate, branchRef, newOID, oldOID)
	observed, observeErr := gitFinalizationOutput(cfg.RepoRoot, "rev-parse", "--verify", branchRef)
	observed = strings.TrimSpace(observed)
	actual := map[string]string{effect.Key(): observed}
	if markErr := store.MarkTransitionApplied(record, actual); markErr != nil {
		return errors.Join(applyErr, observeErr, markErr)
	}
	loaded, _, loadErr := store.LoadTransition(record.TransitionID)
	if loadErr != nil {
		return errors.Join(applyErr, observeErr, loadErr)
	}
	after, snapshotErr := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if snapshotErr != nil {
		return errors.Join(applyErr, observeErr, snapshotErr)
	}
	classification := store.ClassifyTransition(loaded, actual)[effect.Key()]
	switch classification {
	case controller.EffectExpectedNew:
		if _, commitErr := store.CommitTransition(loaded, actual, false, nil); commitErr != nil {
			return errors.Join(applyErr, observeErr, commitErr)
		}
		if _, finalizeErr := store.FinalizeTransition(loaded); finalizeErr != nil {
			return errors.Join(applyErr, observeErr, finalizeErr)
		}
	case controller.EffectExpectedOld:
		if applyErr == nil {
			_, failErr := store.FailClosed("publication ref update reported success without expected ref effect", loaded.TransitionID, workspace, before, after, actual)
			return errors.Join(fmt.Errorf("publication ref update did not apply expected effect"), failErr)
		}
		if _, cancelErr := store.CancelTransition(loaded, actual); cancelErr != nil {
			return errors.Join(applyErr, observeErr, cancelErr)
		}
	case controller.EffectUnexpected:
		_, failErr := store.FailClosed("publication ref update reached unexpected ref state", loaded.TransitionID, workspace, before, after, actual)
		return errors.Join(applyErr, observeErr, fmt.Errorf("publication ref update reached unexpected state"), failErr)
	default:
		return errors.Join(applyErr, observeErr, fmt.Errorf("publication ref transition classification is unavailable"))
	}
	outcome := "success"
	if applyErr != nil {
		outcome = "error"
	}
	if _, provenanceErr := store.RecordTransitionMutation(admission, "publication-ref-update:"+branchRef, outcome, after); provenanceErr != nil {
		return errors.Join(applyErr, observeErr, provenanceErr)
	}
	return errors.Join(applyErr, observeErr)
}

func updatePublicationRef(repoRoot string, candidate state.PublicationCandidate, branchRef, newOID, oldOID string) error {
	command := exec.Command("git", "-C", repoRoot, "update-ref", branchRef, newOID, oldOID)
	command.Env = append(os.Environ(), publicationRefTransactionEnv+"="+candidate.SnapshotID)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func publicationPromotionHead(repoRoot string) (string, string, *finalizationFailure) {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil || head.Unborn || head.Head == "" || head.Detached || !strings.HasPrefix(head.SymbolicHead, "refs/heads/") {
		return "", "", publicationReadinessFailure(publicationFailurePromotionHead, "current branch HEAD is unavailable")
	}
	return head.SymbolicHead, head.Head, nil
}

func verifyPublicationCandidateCommit(repoRoot string, candidate state.PublicationCandidate) *finalizationFailure {
	tree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", candidate.CommitOID+"^{tree}")
	if err != nil || strings.TrimSpace(tree) != candidate.TreeOID {
		return publicationReadinessFailure(publicationFailureCandidateStale, "candidate commit tree no longer matches publication candidate")
	}
	parent, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", candidate.CommitOID+"^")
	if err != nil || strings.TrimSpace(parent) != candidate.BaseHead {
		return publicationReadinessFailure(publicationFailureCandidateStale, "candidate commit parent no longer matches publication candidate base")
	}
	return nil
}

func blockedPublicationPromotion(candidateOID, reason, detail string) publicationPromotionOutput {
	return publicationPromotionOutput{
		Status:       publicationPromotionStatusBlocked,
		CandidateOID: candidateOID,
		Failure:      publicationReadinessFailure(reason, detail),
	}
}
