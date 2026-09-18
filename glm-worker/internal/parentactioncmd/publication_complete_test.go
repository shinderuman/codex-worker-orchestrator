package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationCompletionReportsMissingGateBeforePromotion(t *testing.T) {
	cfg, st, _ := preparePublicationRuntimeCandidate(t)
	failure := verifyPublicationCompletionGate(cfg, st)
	if failure == nil || failure.Stage != "publication" || failure.Reason != publicationFailureGateMissing {
		t.Fatalf("completion gate = %#v", failure)
	}
}

func TestPublicationCompletionRequiresReadyCandidatePromotion(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("completion candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, prepareFailure := preparePublicationCandidate(cfg, st, "completion publication")
	if prepareFailure != nil {
		t.Fatalf("prepare failed: %#v", prepareFailure)
	}

	failure := verifyPublicationCompletionGate(cfg, st)
	if failure == nil || failure.Reason != publicationFailureNotPromoted {
		t.Fatalf("unpromoted completion gate = %#v", failure)
	}
	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted || promoted.CandidateOID != candidate.CommitOID {
		t.Fatalf("promotion = %#v", promoted)
	}
	if failure := verifyPublicationCompletionGate(cfg, st); failure != nil {
		t.Fatalf("promoted completion was blocked: %#v", failure)
	}
}
