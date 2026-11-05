package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func equivalentReviewAcceptedFixture(t *testing.T) (*StateStore, ParentCompletionOutcome, ModelCallLog, ModelCallLog, SnapshotDigest) {
	t.Helper()
	st, repo, old := newBoundParentReviewTestStore(t)
	openBoundReviewForTest(t, st, old, "review.go:1")
	review, err := st.CurrentParentReviewBinding()
	if err != nil || review == nil {
		t.Fatalf("original review: %#v %v", review, err)
	}
	if err := st.MarkParentReviewEvidence(review.ID, "parent-source-evidence", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "original-source-digest", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("original parent acceptance: %v %v", accepted, err)
	}
	if err := st.SaveControllerRuntimeBinding(ControllerRuntimeBinding{
		AttemptID: "old-attempt", TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "unchanged",
	}); err != nil {
		t.Fatal(err)
	}
	completion, err := st.CurrentParentCompletionOutcome()
	if err != nil || completion == nil || completion.Review == nil || completion.Source == nil {
		t.Fatalf("accepted review outcome: %#v %v", completion, err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	worker := ModelCallLog{CallID: "worker-call", CallType: CallTypeTask, TaskID: taskID,
		Role: WorkerRole, SessionID: "worker-session", PacketStatus: string(packet.StatusImplemented),
		Outcome: "success"}
	reviewer := ModelCallLog{CallID: "reviewer-call", CallType: CallTypeTask, TaskID: taskID,
		Role: ReviewerRole, ModelAlias: "reviewer-model", SessionID: "reviewer-session",
		ReadOnly: true, PacketStatus: string(packet.StatusNeedsSolReview), Outcome: "success",
		ResponseSHA256: "authentic-reviewer-response-digest"}
	st.RecordModelCallLog(worker)
	st.RecordModelCallLog(reviewer)
	if err := os.WriteFile(filepath.Join(repo, "metadata.md"), []byte("new metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewTestCommand(t, repo, "add", "metadata.md")
	gitReviewTestCommand(t, repo, "-c", "user.name=review test", "-c", "user.email=review@example.invalid", "commit", "-q", "-m", "metadata advancement")
	actual, err := CaptureGitSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	current := SnapshotDigest{Head: actual.Head, IndexDigest: actual.IndexDigest, WorktreeDigest: actual.WorktreeDigest}
	return st, *completion, worker, reviewer, current
}

func TestEquivalentReentryPreservesIndependentHighReviewAndReprovesParentEvidence(t *testing.T) {
	st, completion, worker, reviewer, current := equivalentReviewAcceptedFixture(t)
	before, err := st.ReadModelCallLogs(reviewer.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	authority := ControllerReentryAuthority{
		PredecessorAttemptID: "old-attempt", AttemptID: "reentered-attempt",
		TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "unchanged",
		PredecessorTaskContractDigest: "unchanged",
	}
	reentered, err := st.ReenterEquivalentReviewFromController(authority, &completion, reviewer, worker, current)
	if err != nil || !reentered {
		t.Fatalf("verified equivalent review reentry: %v %v", reentered, err)
	}
	open, err := st.CurrentParentReview()
	if err != nil || open == nil || open.Role != string(ReviewerRole) ||
		open.Risk != string(packet.RiskHigh) || open.PacketStatus != string(packet.StatusNeedsSolReview) {
		t.Fatalf("source rebind lowered HIGH Reviewer result: %#v %v", open, err)
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil || binding == nil {
		t.Fatal(err)
	}
	if binding.ID != completion.Review.ID || binding.PacketSHA256 != completion.Review.PacketSHA256 ||
		!reflect.DeepEqual(binding.Targets, completion.Review.Targets) || binding.SolQuestion != completion.Review.SolQuestion ||
		binding.SourceEquivalence == nil || binding.SourceEquivalence.ReviewerCallID != reviewer.CallID ||
		binding.SourceEquivalence.WorkerCallID != worker.CallID || binding.SourceEquivalence.OriginalSnapshot != completion.Review.Snapshot ||
		binding.Snapshot != current || binding.Proof != nil || binding.Coverage != nil {
		t.Fatalf("Reviewer packet/provenance mutated or stale parent proof reused: %#v", binding)
	}
	if ready, err := st.ParentReviewAcceptReady(); err != nil || ready {
		t.Fatalf("parent evidence must be re-proven: ready=%v err=%v", ready, err)
	}
	if err := st.MarkParentReviewEvidence(binding.ID, "new-parent-source-proof", []ParentReviewEvidenceClaim{{
		Kind: "source", Digest: "new-source", Locator: "review.go:1-1",
	}}); err != nil {
		t.Fatal(err)
	}
	if ready, err := st.ParentReviewAcceptReady(); err != nil || !ready {
		t.Fatalf("independently re-proven target should allow parent decision: ready=%v err=%v", ready, err)
	}
	after, err := st.ReadModelCallLogs(reviewer.TaskID)
	if err != nil || len(after) != len(before) {
		t.Fatalf("reentry produced a fake Reviewer model call: before=%d after=%d err=%v", len(before), len(after), err)
	}
}

func TestEquivalentReentryRejectsForeignOrUnprovenReview(t *testing.T) {
	t.Run("foreign source acceptance", func(t *testing.T) {
		st, completion, worker, reviewer, current := equivalentReviewAcceptedFixture(t)
		completion.Review.PacketSHA256 = "foreign"
		_, err := st.ReenterEquivalentReviewFromController(ControllerReentryAuthority{
			PredecessorAttemptID: "old-attempt", AttemptID: "reentered",
			TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "unchanged", PredecessorTaskContractDigest: "unchanged",
		}, &completion, reviewer, worker, current)
		if err == nil || st.TaskStatus() != TaskStatusAwaitingParentCompletion {
			t.Fatalf("forged packet admitted: %v", err)
		}
	})
	t.Run("incomplete model evidence", func(t *testing.T) {
		st, completion, worker, reviewer, current := equivalentReviewAcceptedFixture(t)
		reviewer.CallID = ""
		_, err := st.ReenterEquivalentReviewFromController(ControllerReentryAuthority{
			PredecessorAttemptID: "old-attempt", AttemptID: "reentered",
			TaskPath: "IMPLEMENTATION_TASKS/active.md", TaskContractDigest: "unchanged", PredecessorTaskContractDigest: "unchanged",
		}, &completion, reviewer, worker, current)
		if err == nil || st.TaskStatus() != TaskStatusAwaitingParentCompletion {
			t.Fatalf("missing Reviewer provenance admitted: %v", err)
		}
	})
}
