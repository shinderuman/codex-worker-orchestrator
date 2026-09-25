package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRiskFloorFailClosedTargetsProduceParentDiffClaim(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("base\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	passPkt := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"reviewer pass","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)
	enforced, err := w.riskFloorFailClosedResult(passPkt)
	if err != nil {
		t.Fatal(err)
	}
	if len(enforced.Targets) != 1 || enforced.Targets[0] != "tracked.md:diff" {
		t.Fatalf("targets = %#v", enforced.Targets)
	}
	if err := validateTypedResult(enforced); err != nil {
		t.Fatalf("synthesized fail-closed packet invalid: %v", err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	digest := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	if err := st.RecordSolResultWithReviewSnapshot(enforced, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, digest); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := parentevidence.PrintReviewEvidence(repo, st, &stdout); err != nil {
		t.Fatal(err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil || binding.Proof == nil {
		t.Fatalf("review proof = %#v err=%v", binding, err)
	}
	if len(binding.Proof.Claims) != 1 || binding.Proof.Claims[0].Kind != "diff" {
		t.Fatalf("review proof claims = %#v", binding.Proof.Claims)
	}
	if !strings.Contains(stdout.String(), "+changed") {
		t.Fatalf("projected evidence did not expose actual task diff: %s", stdout.String())
	}
}

func TestRiskFloorFailClosedDoesNotSubstituteFallbackTarget(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	passPkt := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"reviewer pass","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if _, err := w.riskFloorFailClosedResult(passPkt); err == nil || !strings.Contains(err.Error(), "no changed paths") {
		t.Fatalf("empty task diff should fail closed without substitute target: %v", err)
	}
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, errors.New("changed paths unavailable") }
	if _, err := w.riskFloorFailClosedResult(passPkt); err == nil || !strings.Contains(err.Error(), "changed paths unavailable") {
		t.Fatalf("changed-path failure should propagate instead of substituting target: %v", err)
	}
}
