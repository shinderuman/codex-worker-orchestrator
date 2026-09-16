package parentactioncmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewEvidenceParentActionProjectsOpenReviewTargets(t *testing.T) {
	repoRoot := t.TempDir()
	gitReviewEvidenceCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar target = 1\nvar third = 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewEvidenceCommand(t, repoRoot, "add", "review.go")
	gitReviewEvidenceCommand(t, repoRoot, "-c", "user.name=review evidence test", "-c", "user.email=review-evidence@example.invalid", "commit", "-q", "-m", "seed")

	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "parent-review-evidence-action", RepoRoot: repoRoot}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the current target and decide whether to accept.",
		Targets:     []string{"review.go:2-3(exact target)"},
	}
	if err := st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, state.SnapshotDigest{
		Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest,
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := executeWithTerminalEnvelope(cfg, []string{actionReviewEvidence}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("review-evidence returned no machine output")
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || !ready {
		t.Fatalf("review accept readiness = %v err=%v", ready, err)
	}
}

func TestReviewEvidenceParentActionRejectsArguments(t *testing.T) {
	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "parent-review-evidence-action", RepoRoot: t.TempDir()}
	if err := executeWithTerminalEnvelope(cfg, []string{actionReviewEvidence, "extra"}, io.Discard, io.Discard); err == nil {
		t.Fatal("review-evidence accepted unexpected arguments")
	}
}

func gitReviewEvidenceCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
