package parentactioncmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationPrepareOutput struct {
	Status    string                      `json:"status"`
	Candidate *state.PublicationCandidate `json:"candidate,omitempty"`
	Failure   *finalizationFailure        `json:"failure,omitempty"`
}

const (
	publicationPrepareStatusPrepared = "prepared"
	publicationPrepareStatusBlocked  = "blocked"

	publicationFailureHarnessInactive = "publication_repository_harness_inactive"
	publicationFailureActionNotReady  = "publication_parent_action_not_ready"
	publicationFailureSourceDirty     = "publication_candidate_has_unstaged_changes"
	publicationFailureSourceUntracked = "publication_candidate_has_untracked_files"
	publicationFailureSourceEmpty     = "publication_candidate_has_no_staged_changes"
	publicationFailureGitIdentity     = "publication_candidate_git_identity_unavailable"
	publicationFailureCandidateWrite  = "publication_candidate_commit_failed"
	publicationFailureState           = "publication_candidate_state_failed"
)

func runPublicationPrepare(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 3 || args[0] != "prepare" || args[1] != "--message" || strings.TrimSpace(args[2]) == "" {
		return fmt.Errorf("usage: glm-parent-action push-binding prepare --message <commit-message>")
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

	candidate, failure := preparePublicationCandidate(cfg, st, args[2])
	if failure != nil {
		return json.NewEncoder(stdout).Encode(publicationPrepareOutput{Status: publicationPrepareStatusBlocked, Failure: failure})
	}
	return json.NewEncoder(stdout).Encode(publicationPrepareOutput{Status: publicationPrepareStatusPrepared, Candidate: &candidate})
}

func preparePublicationCandidate(cfg config.AppConfig, st *state.StateStore, message string) (state.PublicationCandidate, *finalizationFailure) {
	if failure := publicationCandidateAdmission(cfg, st); failure != nil {
		return state.PublicationCandidate{}, failure
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil || snapshot.Head == "" {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureGitIdentity, errorDetail(err, "HEAD is unavailable"))
	}
	if failure := publicationCandidateSourceGuard(cfg.RepoRoot); failure != nil {
		return state.PublicationCandidate{}, failure
	}
	treeOID, err := gitFinalizationOutput(cfg.RepoRoot, "write-tree")
	if err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureGitIdentity, err.Error())
	}
	treeOID = strings.TrimSpace(treeOID)
	headTree, err := gitFinalizationOutput(cfg.RepoRoot, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureGitIdentity, err.Error())
	}
	if treeOID == strings.TrimSpace(headTree) {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureSourceEmpty, "candidate tree matches current HEAD")
	}
	messageDigest := publicationMessageDigest(message)
	digest := state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest)
	source := state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
	if existing, err := st.LoadPublicationCandidate(); err == nil && publicationCandidateReusable(cfg.RepoRoot, existing, source, digest, treeOID, messageDigest) {
		return existing, nil
	}
	commitOID, err := createPublicationCandidateCommit(cfg.RepoRoot, treeOID, snapshot.Head, message)
	if err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureCandidateWrite, err.Error())
	}
	taskID, err := st.TaskID()
	if err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureState, err.Error())
	}
	candidate := state.PublicationCandidate{
		Version:       1,
		TaskID:        taskID,
		BaseHead:      snapshot.Head,
		CommitOID:     commitOID,
		TreeOID:       treeOID,
		MessageDigest: messageDigest,
		Snapshot:      source,
		SnapshotID:    digest,
		PreparedAt:    time.Now().UTC(),
	}
	if err := st.ClearRuntimeInstallEvidence(); err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureState, err.Error())
	}
	if err := st.SavePublicationCandidate(candidate); err != nil {
		return state.PublicationCandidate{}, publicationCandidateFailure(publicationFailureState, err.Error())
	}
	return candidate, nil
}

func publicationCandidateAdmission(cfg config.AppConfig, st *state.StateStore) *finalizationFailure {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return publicationCandidateFailure(publicationFailureHarnessInactive, err.Error())
	}
	if !decision.Active {
		return publicationCandidateFailure(publicationFailureHarnessInactive, decision.Reason)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		return publicationCandidateFailure(publicationFailureActionNotReady, err.Error())
	}
	if !plan.Allows(state.ParentActionInstall) && !plan.Allows(state.ParentActionComplete) {
		return publicationCandidateFailure(publicationFailureActionNotReady, "required action is "+string(plan.RequiredAction))
	}
	return nil
}

func publicationCandidateSourceGuard(repoRoot string) *finalizationFailure {
	unstaged := exec.Command("git", "-C", repoRoot, "diff", "--quiet", "--no-ext-diff", "--")
	if err := unstaged.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return publicationCandidateFailure(publicationFailureSourceDirty, "worktree differs from staged candidate")
		}
		return publicationCandidateFailure(publicationFailureGitIdentity, err.Error())
	}
	untracked, err := exec.Command("git", "-C", repoRoot, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return publicationCandidateFailure(publicationFailureGitIdentity, err.Error())
	}
	if len(untracked) != 0 {
		return publicationCandidateFailure(publicationFailureSourceUntracked, "untracked files are outside the staged candidate")
	}
	staged, err := exec.Command("git", "-C", repoRoot, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMRD").Output()
	if err != nil {
		return publicationCandidateFailure(publicationFailureGitIdentity, err.Error())
	}
	if len(staged) == 0 {
		return publicationCandidateFailure(publicationFailureSourceEmpty, "no staged candidate changes")
	}
	return nil
}

func createPublicationCandidateCommit(repoRoot, treeOID, parentOID, message string) (string, error) {
	command := exec.Command("git", "-C", repoRoot, "commit-tree", treeOID, "-p", parentOID)
	command.Stdin = strings.NewReader(message + "\n")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	commitOID := strings.TrimSpace(string(output))
	if len(commitOID) != 40 {
		return "", fmt.Errorf("unexpected candidate commit OID %q", commitOID)
	}
	return commitOID, nil
}

func publicationCandidateReusable(repoRoot string, candidate state.PublicationCandidate, snapshot state.SnapshotDigest, snapshotID, treeOID, messageDigest string) bool {
	if candidate.Snapshot != snapshot || candidate.SnapshotID != snapshotID || candidate.TreeOID != treeOID || candidate.MessageDigest != messageDigest {
		return false
	}
	commitTree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", candidate.CommitOID+"^{tree}")
	if err != nil || strings.TrimSpace(commitTree) != treeOID {
		return false
	}
	parent, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", candidate.CommitOID+"^")
	return err == nil && strings.TrimSpace(parent) == candidate.BaseHead
}

func publicationMessageDigest(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

func publicationCandidateFailure(reason, detail string) *finalizationFailure {
	return &finalizationFailure{Stage: "candidate", Reason: reason, Detail: compactFinalizationDiagnostic(detail)}
}

func errorDetail(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
