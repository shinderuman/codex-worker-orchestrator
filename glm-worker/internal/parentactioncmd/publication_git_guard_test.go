package parentactioncmd

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationRefGuardAllowsExactReadyCandidateWithTransactionAuthority(t *testing.T) {
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
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	t.Setenv(publicationRefTransactionEnv, candidate.SnapshotID)
	if err := verifyPublicationRefUpdate(cfg, candidate.BaseHead, candidate.CommitOID, branchRef); err != nil {
		t.Fatalf("exact ready candidate promotion rejected: %v", err)
	}
}

func TestPublicationRefGuardAllowsOrdinaryLocalRefUpdateWithoutCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	t.Setenv(publicationRefTransactionEnv, "")
	branchRef, headOID, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	if err := verifyPublicationRefUpdate(cfg, headOID, strings.Repeat("f", 40), branchRef); err != nil {
		t.Fatalf("ordinary local ref update without publication candidate was rejected: %v", err)
	}
}

func TestPublicationRefGuardAllowsOrdinaryLocalRefUpdateWhileCandidateExists(t *testing.T) {
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
	t.Setenv(publicationRefTransactionEnv, "")
	branchRef, headOID, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	ordinaryOID := strings.Repeat("f", 40)
	if ordinaryOID == candidate.CommitOID {
		ordinaryOID = strings.Repeat("e", 40)
	}
	if err := verifyPublicationRefUpdate(cfg, headOID, ordinaryOID, branchRef); err != nil {
		t.Fatalf("ordinary local ref update was treated as publication promotion: %v", err)
	}
}

func TestPublicationRefGuardRejectsUnboundExactCandidatePromotion(t *testing.T) {
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
	t.Setenv(publicationRefTransactionEnv, "")
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	if err := verifyPublicationRefUpdate(cfg, candidate.BaseHead, candidate.CommitOID, branchRef); err == nil || !strings.Contains(err.Error(), "transaction authority missing") {
		t.Fatalf("unbound exact candidate promotion was admitted: %v", err)
	}
}

func TestPublicationRefGuardRejectsBoundNonCandidateMutation(t *testing.T) {
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
	t.Setenv(publicationRefTransactionEnv, candidate.SnapshotID)
	if err := verifyPublicationRefUpdate(cfg, headOID, wrongOID, branchRef); err == nil || !strings.Contains(err.Error(), "does not match exact candidate promotion or rollback") {
		t.Fatalf("bound non-candidate mutation was admitted: %v", err)
	}
}

func TestPublicationRefGuardRejectsExactCandidateWhenRequiredGateMissing(t *testing.T) {
	cfg, _, candidate := preparePublicationRuntimeCandidate(t)
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	t.Setenv(publicationRefTransactionEnv, candidate.SnapshotID)
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
	t.Setenv(publicationRefTransactionEnv, candidate.SnapshotID)
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
