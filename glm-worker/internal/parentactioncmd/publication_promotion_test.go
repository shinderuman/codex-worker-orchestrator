package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationPromotionAdvancesOnlyToExactReadyCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("ready candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")
	candidate, failure := preparePublicationCandidate(cfg, st, "ready publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}

	output := promotePublicationCandidate(cfg, st)
	if output.Status != publicationPromotionStatusPromoted || output.CandidateOID != candidate.CommitOID || output.Failure != nil {
		t.Fatalf("promotion = %#v", output)
	}
	if output.BranchRef == "" {
		t.Fatal("promotion did not report branch ref")
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != candidate.CommitOID {
		t.Fatalf("HEAD = %s, want candidate %s", got, candidate.CommitOID)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", candidate.CommitOID+"^"); got != baseHead {
		t.Fatalf("candidate parent = %s, want %s", got, baseHead)
	}
	if !pushBindingTreeClean(cfg.RepoRoot) {
		t.Fatal("promotion did not leave exact candidate tree clean")
	}

	repeated := promotePublicationCandidate(cfg, st)
	if repeated.Status != publicationPromotionStatusPromoted || repeated.CandidateOID != candidate.CommitOID || repeated.Failure != nil {
		t.Fatalf("idempotent promotion = %#v", repeated)
	}
}

func TestPublicationPromotionRejectsMissingRequiredInstallEvidence(t *testing.T) {
	cfg, st, candidate := preparePublicationRuntimeCandidate(t)
	baseHead := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")

	output := promotePublicationCandidate(cfg, st)
	if output.Status != publicationPromotionStatusBlocked || output.CandidateOID != candidate.CommitOID || output.Failure == nil {
		t.Fatalf("promotion = %#v", output)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != baseHead {
		t.Fatalf("blocked promotion advanced HEAD: %s != %s", got, baseHead)
	}
}

func TestPublicationPromotionReadyReentryRollsBackInvalidCandidate(t *testing.T) {
	cfg, _, candidate, branchRef := publicationPromotionAtomicityFixture(t)
	publicationGit(t, cfg.RepoRoot, "update-ref", branchRef, candidate.CommitOID, candidate.BaseHead)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("reentry mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure == nil {
		t.Fatal("reentry mutation did not invalidate promoted candidate")
	}

	output := promoteReadyPublicationCandidate(cfg, candidate)
	if output.Status != publicationPromotionStatusBlocked || output.Failure == nil || output.Failure.Reason != publicationFailurePromotionHead {
		t.Fatalf("reentry promotion = %#v", output)
	}
	if output.BranchRef != branchRef {
		t.Fatalf("reentry branch ref = %q, want %q", output.BranchRef, branchRef)
	}
	if got := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"); got != candidate.BaseHead {
		t.Fatalf("blocked reentry left advanced HEAD: %s != base %s", got, candidate.BaseHead)
	}
}

func publicationPromotionAtomicityFixture(t *testing.T) (config.AppConfig, *state.StateStore, state.PublicationCandidate, string) {
	t.Helper()
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("atomic candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "atomic publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("promotion head = %#v", headFailure)
	}
	return cfg, st, candidate, branchRef
}
