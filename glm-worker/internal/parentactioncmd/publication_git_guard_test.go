package parentactioncmd

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationRefGuardAllowsOnlyExactReadyCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, cfg.RepoRoot, "README.md", "guard candidate\n")
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "guard publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	branchRef, headOID, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	wrongOID := strings.Repeat("f", 40)
	if wrongOID == candidate.CommitOID {
		wrongOID = strings.Repeat("e", 40)
	}
	if err := verifyPublicationRefUpdate(cfg, headOID, wrongOID, branchRef); err == nil {
		t.Fatal("non-candidate ref update was admitted")
	}
	if err := verifyPublicationRefUpdate(cfg, candidate.BaseHead, candidate.CommitOID, branchRef); err != nil {
		t.Fatalf("exact ready candidate promotion rejected: %v", err)
	}
}

func TestPublicationRefGuardRejectsExactCandidateWhenRequiredGateMissing(t *testing.T) {
	cfg, _, candidate := preparePublicationRuntimeCandidate(t)
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	if err := verifyPublicationRefUpdate(cfg, candidate.BaseHead, candidate.CommitOID, branchRef); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("candidate with missing install gate was admitted: %v", err)
	}
}

func TestPublicationRefGuardAllowsRollbackOnlyForInvalidPromotedCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, cfg.RepoRoot, "README.md", "rollback guard candidate\n")
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "rollback guard publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted || promoted.Failure != nil || promoted.BranchRef == "" {
		t.Fatalf("promotion = %#v", promoted)
	}
	if err := verifyPublicationRefUpdate(cfg, candidate.CommitOID, candidate.BaseHead, promoted.BranchRef); err == nil || !strings.Contains(err.Error(), "remains valid") {
		t.Fatalf("valid promotion rollback was admitted: %v", err)
	}
	writePushBindingFile(t, cfg.RepoRoot, "README.md", "invalid after promotion\n")
	if failure := publicationPromotionPostcondition(cfg.RepoRoot, candidate); failure == nil {
		t.Fatal("dirty promoted candidate remained valid")
	}
	if err := verifyPublicationRefUpdate(cfg, candidate.CommitOID, candidate.BaseHead, promoted.BranchRef); err != nil {
		t.Fatalf("invalid exact promotion rollback rejected: %v", err)
	}
}
