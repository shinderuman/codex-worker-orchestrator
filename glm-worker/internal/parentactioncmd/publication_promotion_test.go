package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

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
