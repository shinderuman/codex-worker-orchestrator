package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationGuardsStayActiveAfterPinnedHarnessMarkerRemoval(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, cfg.RepoRoot, "README.md", "pinned publication\n")
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "pinned publication")
	if failure != nil {
		t.Fatalf("prepare failed: %#v", failure)
	}
	branchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
	if headFailure != nil {
		t.Fatalf("head = %#v", headFailure)
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, repositoryharness.MarkerPath)); err != nil {
		t.Fatal(err)
	}

	if err := verifyPublicationRefUpdate(cfg, candidate.BaseHead, candidate.CommitOID, branchRef); err == nil {
		t.Fatal("pinned active task admitted ref update after harness marker removal")
	}
	remote := applyPublicationRemoteWriteGuardForTask(cfg, st, publicationRemoteWriteFixture(candidate.BaseHead, false))
	if remote.Status != publicationPrepareStatusBlocked || remote.RemoteWrite != nil || remote.Failure == nil {
		t.Fatalf("pinned active task admitted remote write after harness marker removal: %#v", remote)
	}
	if failure := verifyPublicationCompletionGate(cfg, st); failure == nil {
		t.Fatal("pinned active task admitted completion after harness marker removal")
	}
}
