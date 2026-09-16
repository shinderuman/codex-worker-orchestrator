package state

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentReviewAcceptRequiresCurrentEvidenceProof(t *testing.T) {
	st, _, snapshot := newBoundParentReviewTestStore(t)
	openBoundReviewForTest(t, st, snapshot, "review.go:1")

	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview || plan.Allows(ParentActionAccept) || plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("review plan before evidence = %#v", plan)
	}
	if !plan.Allows(ParentActionFix) || !plan.Allows(ParentActionPark) {
		t.Fatalf("review plan lost fix/park before evidence = %#v", plan)
	}

	accepted, err := st.AcceptParentReview()
	if err == nil || accepted {
		t.Fatalf("accept without evidence = %v err=%v", accepted, err)
	}
	if !strings.Contains(err.Error(), "requires matching current target evidence") {
		t.Fatalf("unexpected accept error: %v", err)
	}

	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if err := st.MarkParentReviewEvidence(binding.ID, "parent-evidence-call", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "digest", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}

	plan, err = st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionAccept) || !plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("review plan after evidence = %#v", plan)
	}

	accepted, err = st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept with matching evidence = %v err=%v", accepted, err)
	}
	if st.TaskStatus() != TaskStatusAwaitingParentCompletion {
		t.Fatalf("status after accept = %s", st.TaskStatus())
	}
}

func TestParentReviewEvidenceProofInvalidatedBySnapshotChange(t *testing.T) {
	st, repoRoot, snapshot := newBoundParentReviewTestStore(t)
	openBoundReviewForTest(t, st, snapshot, "review.go:1")
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("binding = %#v err=%v", binding, err)
	}
	if err := st.MarkParentReviewEvidence(binding.ID, "parent-evidence-call", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "digest", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\n\nvar changed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ready, err := st.ParentReviewAcceptReady()
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("snapshot change reused stale review evidence")
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allows(ParentActionAccept) || plan.AdmitsCommand(ParentActionAccept) {
		t.Fatalf("stale evidence advertised accept = %#v", plan)
	}
	if accepted, err := st.AcceptParentReview(); err == nil || accepted {
		t.Fatalf("accept after snapshot change = %v err=%v", accepted, err)
	}
}

func TestParentReviewEvidenceProofBoundToReviewIdentity(t *testing.T) {
	st, _, snapshot := newBoundParentReviewTestStore(t)
	openBoundReviewForTest(t, st, snapshot, "review.go:1")
	first, err := st.CurrentParentReviewBinding()
	if err != nil || first == nil {
		t.Fatalf("first binding = %#v err=%v", first, err)
	}
	if err := st.MarkParentReviewEvidence(first.ID, "first-call", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "first-digest", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}

	openBoundReviewForTest(t, st, snapshot, "review.go:1")
	second, err := st.CurrentParentReviewBinding()
	if err != nil || second == nil {
		t.Fatalf("second binding = %#v err=%v", second, err)
	}
	if second.ID == first.ID {
		t.Fatalf("review identity was reused: %s", second.ID)
	}
	if second.Proof != nil {
		t.Fatalf("new review inherited old proof: %#v", second.Proof)
	}
	if accepted, err := st.AcceptParentReview(); err == nil || accepted {
		t.Fatalf("new review accepted with stale proof = %v err=%v", accepted, err)
	}
	if err := st.MarkParentReviewEvidence(first.ID, "stale-call", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "stale", Locator: "review.go:1-1",
	}}); err == nil {
		t.Fatal("stale review identity was allowed to record proof")
	}
}

func TestParentReviewEvidenceMissingBindingFailsClosed(t *testing.T) {
	st, _, _ := newBoundParentReviewTestStore(t)
	if err := st.RecordSolResult(packet.Result{
		Status: packet.StatusNeedsSolReview,
		Risk:   packet.RiskHigh,
	}, ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := st.AcceptParentReview(); err == nil || accepted {
		t.Fatalf("unbound review accepted = %v err=%v", accepted, err)
	}
}

func TestParentReviewEvidenceDoesNotChangeOtherAcceptSemantics(t *testing.T) {
	t.Run("PASS", func(t *testing.T) {
		st, _, _ := newBoundParentReviewTestStore(t)
		if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}
		accepted, err := st.AcceptParentReview()
		if err != nil || !accepted {
			t.Fatalf("PASS accept = %v err=%v", accepted, err)
		}
	})

	t.Run("NEEDS_SOL_DECISION", func(t *testing.T) {
		st, _, _ := newBoundParentReviewTestStore(t)
		if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{}); err != nil {
			t.Fatal(err)
		}
		accepted, err := st.AcceptParentReview()
		if err == nil || accepted || !strings.Contains(err.Error(), "pending Sol decision") {
			t.Fatalf("decision accept = %v err=%v", accepted, err)
		}
	})
}

func TestNonConvergenceMarkerClearsOnReviewerCompletionAndAcceptRestoresViaEvidence(t *testing.T) {
	st, _, snapshot := newBoundParentReviewTestStore(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishParentValidationNonConvergence(
		packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh},
		ParentReviewProducer{Role: string(WorkerRole), Model: "opus"},
	); err != nil {
		t.Fatal(err)
	}

	duringFailure, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if duringFailure.Allows(ParentActionAccept) || duringFailure.AdmitsCommand(ParentActionAccept) || !duringFailure.Allows(ParentActionFix) || !duringFailure.Allows(ParentActionPark) {
		t.Fatalf("non-convergence admission = %#v", duringFailure)
	}

	openBoundReviewForTest(t, st, snapshot, "review.go:1")
	open, err := st.CurrentParentReview()
	if err != nil || open == nil || open.ParentValidationNonConvergence {
		t.Fatalf("reviewer completion must drop the non-convergence marker: %#v err=%v", open, err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatalf("reviewer completion must open a normal bound review: %#v err=%v", binding, err)
	}
	beforeEvidence, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if beforeEvidence.Allows(ParentActionAccept) || !beforeEvidence.Allows(ParentActionFix) || !beforeEvidence.Allows(ParentActionPark) {
		t.Fatalf("post-completion admission must be the normal evidence-gated review = %#v", beforeEvidence)
	}

	if err := st.MarkParentReviewEvidence(binding.ID, "parent-evidence-call", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "digest", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}
	restored, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Allows(ParentActionAccept) || !restored.AdmitsCommand(ParentActionAccept) || !restored.Allows(ParentActionFix) {
		t.Fatalf("evidence admission must restore the normal accept/fix admission = %#v", restored)
	}
	current, err := st.TaskID()
	if err != nil || current != taskID {
		t.Fatalf("task identity changed across the recovery loop: %s want %s err=%v", current, taskID, err)
	}
	if _, err := st.LoadResumeCheckpoint(); !errors.Is(err, ErrNoResumeCheckpoint) {
		t.Fatalf("recovery loop must not leave a resume checkpoint: %v", err)
	}
}

func newBoundParentReviewTestStore(t *testing.T) (*StateStore, string, SnapshotDigest) {
	t.Helper()
	repoRoot := t.TempDir()
	gitReviewTestCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewTestCommand(t, repoRoot, "add", "review.go")
	gitReviewTestCommand(t, repoRoot, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-q", "-m", "seed")

	st := &StateStore{dir: t.TempDir()}
	if err := st.Write("repo-root", repoRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return st, repoRoot, SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func openBoundReviewForTest(t *testing.T, st *StateStore, snapshot SnapshotDigest, target string) {
	t.Helper()
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the current target and decide whether to accept.",
		Targets:     []string{target},
	}
	if err := st.RecordSolResultWithReviewSnapshot(result, ParentReviewProducer{Role: string(ReviewerRole), Model: "reviewer"}, snapshot); err != nil {
		t.Fatal(err)
	}
}

func gitReviewTestCommand(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
