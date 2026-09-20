package parentactioncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type publicationRecoverOutput struct {
	Status        string                      `json:"status"`
	Candidate     *state.PublicationCandidate `json:"candidate,omitempty"`
	Safety        string                      `json:"safety,omitempty"`
	HistoryImpact string                      `json:"history_impact,omitempty"`
	NextAction    *app.PublicationActionSpec  `json:"next_action,omitempty"`
	Failure       *finalizationFailure        `json:"failure,omitempty"`
}

const publicationRecoverSubcommand = "re" + "cover"

const (
	publicationRecoverStatusRecovered = "recovered"
	publicationRecoverStatusBlocked   = "blocked"

	publicationRecoverSafety = "adoption registers the existing commit as the publication candidate without modifying refs, history, or the working tree"

	publicationFailureRecoverStatus       = "publication_recovery_status_invalid"
	publicationFailureRecoverCandidate    = "publication_recovery_candidate_present"
	publicationFailureRecoverTree         = "publication_recovery_tree_not_clean"
	publicationFailureRecoverHead         = "publication_recovery_head_invalid"
	publicationFailureRecoverRemote       = "publication_recovery_remote_advanced"
	publicationFailureRecoverUnverifiable = "publication_recovery_remote_unverifiable"
	publicationFailureRecoverGuard        = "publication_recovery_guard_setup_invalid"
	publicationFailureRecoverState        = "publication_recovery_state_failed"
)

func runPublicationRecover(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != publicationRecoverSubcommand {
		return fmt.Errorf("usage: glm-parent-action push-binding recover")
	}
	return runPublicationLocked(cfg, func(cfg config.AppConfig, st *state.StateStore) error {
		return json.NewEncoder(stdout).Encode(recoverPublicationCandidate(cfg, st))
	})
}

func recoverPublicationCandidate(cfg config.AppConfig, st *state.StateStore) publicationRecoverOutput {
	if failure := publicationRecoverAdmission(cfg, st); failure != nil {
		return publicationRecoverBlocked(*failure)
	}
	head, baseHead, failure := publicationRecoverHead(cfg.RepoRoot, st)
	if failure != nil {
		return publicationRecoverBlocked(*failure)
	}
	if failure := verifyPublicationRecoverRemote(cfg.RepoRoot, head); failure != nil {
		return publicationRecoverBlocked(*failure)
	}
	candidate, failure := adoptPublicationCandidate(cfg, st, head, baseHead)
	if failure != nil {
		return publicationRecoverBlocked(*failure)
	}
	return publicationRecoverOutput{
		Status:        publicationRecoverStatusRecovered,
		Candidate:     &candidate,
		Safety:        publicationRecoverSafety,
		HistoryImpact: "commit " + candidate.CommitOID + " remains exactly as committed; no reset or rewrite occurs",
		NextAction:    app.ProjectPublicationSequence(cfg.RepoRoot, st).NextAction,
	}
}

func publicationRecoverAdmission(cfg config.AppConfig, st *state.StateStore) *finalizationFailure {
	if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		return publicationRecoverFailure(publicationFailureRecoverStatus, "bounded recovery admits only awaiting parent completion tasks")
	}
	if failure := publicationCandidateAdmission(cfg, st); failure != nil {
		failure.Stage = "recovery"
		return failure
	}
	if err := workflow.VerifyPublicationGuardSetup(cfg.RepoRoot); err != nil {
		return publicationRecoverFailure(publicationFailureRecoverGuard, err.Error())
	}
	if _, err := st.LoadPublicationCandidate(); err == nil {
		return publicationRecoverFailure(publicationFailureRecoverCandidate, "publication candidate already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	if !pushBindingTreeClean(cfg.RepoRoot) {
		return publicationRecoverFailure(publicationFailureRecoverTree, "recovery adoption requires a clean committed tree")
	}
	return nil
}

func publicationRecoverHead(repoRoot string, st *state.StateStore) (string, string, *finalizationFailure) {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil || head.Unborn || head.Head == "" || head.Detached || !strings.HasPrefix(head.SymbolicHead, "refs/heads/") {
		return "", "", publicationRecoverFailure(publicationFailureRecoverHead, "recovery adoption requires a resolvable local branch HEAD")
	}
	baseline := st.ReadOr("baseline-head", "")
	if failure := verifyPublicationRecoverBaseline(repoRoot, head.Head, baseline); failure != nil {
		return "", "", failure
	}
	return head.Head, baseline, nil
}

func verifyPublicationRecoverBaseline(repoRoot, headOID, baseline string) *finalizationFailure {
	if baseline == "" || headOID == baseline {
		return publicationRecoverFailure(publicationFailureRecoverHead, "accepted task baseline does not classify a committed recovery source")
	}
	if failure := verifyPublicationRecoverSingleCommit(repoRoot, baseline); failure != nil {
		return failure
	}
	return verifyPublicationRecoverNonEmptySource(repoRoot, baseline)
}

func verifyPublicationRecoverSingleCommit(repoRoot, baseline string) *finalizationFailure {
	parents, err := gitFinalizationOutput(repoRoot, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil || len(strings.Fields(parents)) != 2 || strings.Fields(parents)[1] != baseline {
		return publicationRecoverFailure(publicationFailureRecoverHead, "committed state is not a single non-merge commit exactly on the accepted baseline")
	}
	return nil
}

func verifyPublicationRecoverNonEmptySource(repoRoot, baseline string) *finalizationFailure {
	tree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return publicationRecoverFailure(publicationFailureRecoverHead, err.Error())
	}
	baseTree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", baseline+"^{tree}")
	if err != nil {
		return publicationRecoverFailure(publicationFailureRecoverHead, err.Error())
	}
	if strings.TrimSpace(tree) == strings.TrimSpace(baseTree) {
		return publicationRecoverFailure(publicationFailureRecoverHead, "committed state is an empty publication source")
	}
	return nil
}

func verifyPublicationRecoverRemote(repoRoot, headOID string) *finalizationFailure {
	branch := strings.TrimPrefix(publicationRecoverBranch(repoRoot), "refs/heads/")
	upstream, err := resolveGitUpstream(repoRoot, branch)
	if err != nil {
		if errors.Is(err, errGitUpstreamMissing) {
			return nil
		}
		return publicationRecoverFailure(publicationFailureRecoverUnverifiable, err.Error())
	}
	probe := pushBindingProbeRemote(repoRoot, upstream.RemoteName, upstream.RemoteRef)
	if probe.State != pushBindingProbeRead {
		return publicationRecoverFailure(publicationFailureRecoverUnverifiable, "publication remote state cannot be observed")
	}
	if probe.RemoteOID == headOID {
		return publicationRecoverFailure(publicationFailureRecoverRemote, "committed state is already present on the publication remote")
	}
	return nil
}

func publicationRecoverBranch(repoRoot string) string {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil {
		return ""
	}
	return head.SymbolicHead
}

func adoptPublicationCandidate(cfg config.AppConfig, st *state.StateStore, headOID, baseHead string) (state.PublicationCandidate, *finalizationFailure) {
	snapshotRaw, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		return state.PublicationCandidate{}, publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	snapshot := state.SnapshotDigest{
		Head:                          baseHead,
		IndexDigest:                   snapshotRaw.IndexDigest,
		WorktreeDigest:                snapshotRaw.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshotRaw.WorktreeDigestExcludingParent,
	}
	tree, err := gitFinalizationOutput(cfg.RepoRoot, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return state.PublicationCandidate{}, publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	message, err := gitFinalizationOutput(cfg.RepoRoot, "log", "-1", "--format=%B", "HEAD")
	if err != nil {
		return state.PublicationCandidate{}, publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	taskID, err := st.TaskID()
	if err != nil {
		return state.PublicationCandidate{}, publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	candidate := state.PublicationCandidate{
		Version:       1,
		TaskID:        taskID,
		BaseHead:      baseHead,
		CommitOID:     headOID,
		TreeOID:       strings.TrimSpace(tree),
		MessageDigest: publicationMessageDigest(message),
		Snapshot:      snapshot,
		SnapshotID:    state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest),
		PreparedAt:    time.Now().UTC(),
	}
	if err := st.SavePublicationCandidate(candidate); err != nil {
		return state.PublicationCandidate{}, publicationRecoverFailure(publicationFailureRecoverState, err.Error())
	}
	return candidate, nil
}

func publicationRecoverBlocked(failure finalizationFailure) publicationRecoverOutput {
	return publicationRecoverOutput{Status: publicationRecoverStatusBlocked, Failure: &failure}
}

func publicationRecoverFailure(reason, detail string) *finalizationFailure {
	return &finalizationFailure{Stage: "recovery", Reason: reason, Detail: compactFinalizationDiagnostic(detail)}
}
