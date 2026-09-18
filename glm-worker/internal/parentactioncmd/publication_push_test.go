package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPublicationPublisherRequiresPromotionAndPushesExactCandidate(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, remote, "init", "-q", "--bare", "-b", "main")
	publicationGit(t, cfg.RepoRoot, "remote", "add", "origin", remote)
	publicationGit(t, cfg.RepoRoot, "push", "-q", "-u", "origin", "main")
	baseOID := publicationGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")

	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("publication candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicationGit(t, cfg.RepoRoot, "add", "README.md")
	candidate, failure := preparePublicationCandidate(cfg, st, "trusted publisher")
	if failure != nil {
		t.Fatalf("prepare = %#v", failure)
	}

	blocked := publishPublicationCandidate(cfg, st)
	if blocked.Status != publicationPrepareStatusBlocked || blocked.Failure == nil {
		t.Fatalf("unpromoted publish = %#v", blocked)
	}
	if got := publicationGitOutput(t, remote, "rev-parse", "refs/heads/main"); got != baseOID {
		t.Fatalf("blocked publish changed remote: %s != %s", got, baseOID)
	}

	promoted := promotePublicationCandidate(cfg, st)
	if promoted.Status != publicationPromotionStatusPromoted || promoted.CandidateOID != candidate.CommitOID {
		t.Fatalf("promotion = %#v", promoted)
	}
	published := publishPublicationCandidate(cfg, st)
	if !publicationPushAlreadySynced(published) || published.ExpectedOID != candidate.CommitOID {
		t.Fatalf("publish = %#v", published)
	}
	if got := publicationGitOutput(t, remote, "rev-parse", "refs/heads/main"); got != candidate.CommitOID {
		t.Fatalf("remote oid = %s want %s", got, candidate.CommitOID)
	}
	if repeated := publishPublicationCandidate(cfg, st); !publicationPushAlreadySynced(repeated) {
		t.Fatalf("idempotent publish = %#v", repeated)
	}
}
