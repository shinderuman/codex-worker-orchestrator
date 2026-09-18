package parentactioncmd

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const publicationFailureNotPromoted = "publication_candidate_not_promoted"

func verifyPublicationCompletionGate(cfg config.AppConfig, st *state.StateStore) *finalizationFailure {
	active, err := publicationGitGuardActive(cfg)
	if err != nil {
		return publicationReadinessFailure(publicationFailureGateMissing, err.Error())
	}
	if !active || st.TaskStatus() == state.TaskStatusNone {
		return nil
	}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		return publicationReadinessFailure(publicationFailureCandidateMissing, err.Error())
	}

	readiness := projectPublicationReadiness(cfg, st)
	branchRef, headOID, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		return headFailure
	}
	_ = branchRef
	if headOID != candidate.CommitOID {
		if readiness.Status != publicationReadinessReady {
			return readiness.Failure
		}
		return publicationReadinessFailure(publicationFailureNotPromoted, "required Gates passed but exact publication candidate is not promoted")
	}
	if failure := verifyPromotedPublicationReadiness(cfg, st, candidate); failure != nil {
		return failure
	}
	return nil
}
