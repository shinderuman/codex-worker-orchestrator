package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
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
	branchRef, headOID, failure := publicationPromotionHead(cfg.RepoRoot)
	if failure != nil {
		return publicationPromotionOutput{Status: publicationPromotionStatusBlocked, CandidateOID: candidate.CommitOID, Failure: failure}
	}
	if headOID == candidate.CommitOID {
		if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure != nil {
			return publicationPromotionOutput{Status: publicationPromotionStatusBlocked, CandidateOID: candidate.CommitOID, BranchRef: branchRef, Failure: failure}
		}
		return publicationPromotionOutput{Status: publicationPromotionStatusPromoted, CandidateOID: candidate.CommitOID, BranchRef: branchRef}
	}
	if headOID != candidate.BaseHead {
		return blockedPublicationPromotion(candidate.CommitOID, publicationFailurePromotionHead, "current HEAD does not match publication candidate base")
	}
	if failure := verifyPublicationCandidateCommit(cfg.RepoRoot, candidate); failure != nil {
		return publicationPromotionOutput{Status: publicationPromotionStatusBlocked, CandidateOID: candidate.CommitOID, BranchRef: branchRef, Failure: failure}
	}
	if _, err := gitFinalizationOutput(cfg.RepoRoot, "update-ref", branchRef, candidate.CommitOID, candidate.BaseHead); err != nil {
		return blockedPublicationPromotion(candidate.CommitOID, publicationFailurePromotionRef, err.Error())
	}
	if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure != nil {
		return rollbackPublicationPromotion(cfg.RepoRoot, candidate, branchRef, failure)
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

func rollbackPublicationPromotion(repoRoot string, candidate state.PublicationCandidate, branchRef string, cause *finalizationFailure) publicationPromotionOutput {
	if _, err := gitFinalizationOutput(repoRoot, "update-ref", branchRef, candidate.BaseHead, candidate.CommitOID); err != nil {
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
