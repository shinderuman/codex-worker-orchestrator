package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func TestParentReviewBindingRejectsTargetsWithoutCanonicalProofPath(t *testing.T) {
	t.Run("free-form relation", func(t *testing.T) {
		st, _, snapshot := newBoundParentReviewTestStore(t)
		err := recordBoundReviewForContract(st, snapshot, "review.go:AppendRecord-acquireRegistryLock")
		assertReviewTargetAdmissionRejected(t, st, err, "correction=")
	})

	t.Run("unchanged diff", func(t *testing.T) {
		st, _, snapshot := newBoundParentReviewTestStore(t)
		err := recordBoundReviewForContract(st, snapshot, "review.go:@diff")
		assertReviewTargetAdmissionRejected(t, st, err, "[diff-absent]")
	})

	t.Run("untracked diff", func(t *testing.T) {
		st, repoRoot, _ := newBoundParentReviewTestStore(t)
		if err := os.WriteFile(filepath.Join(repoRoot, "new.go"), []byte("package review\nfunc NewAPI() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot := currentReviewSnapshotForContract(t, repoRoot)
		err := recordBoundReviewForContract(st, snapshot, "new.go:@diff")
		assertReviewTargetAdmissionRejected(t, st, err, "[diff-absent]")
	})

	t.Run("local variable is not top-level symbol", func(t *testing.T) {
		st, repoRoot, _ := newBoundParentReviewTestStore(t)
		content := "package review\nfunc Caller() {\n\tLocal := 1\n\t_ = Local\n}\n"
		if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot := currentReviewSnapshotForContract(t, repoRoot)
		err := recordBoundReviewForContract(st, snapshot, "review.go:Local")
		assertReviewTargetAdmissionRejected(t, st, err, "[symbol-not-declared]")
	})
}

func TestParentReviewBindingAdmitsCanonicalProofableTargets(t *testing.T) {
	t.Run("untracked Go symbol", func(t *testing.T) {
		st, repoRoot, _ := newBoundParentReviewTestStore(t)
		if err := os.WriteFile(filepath.Join(repoRoot, "new.go"), []byte("package review\nfunc NewAPI() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot := currentReviewSnapshotForContract(t, repoRoot)
		if err := recordBoundReviewForContract(st, snapshot, "new.go:NewAPI"); err != nil {
			t.Fatal(err)
		}
		binding, err := st.CurrentParentReviewBinding()
		if err != nil || binding == nil || len(binding.Targets) != 1 || binding.Targets[0] != "new.go:NewAPI" {
			t.Fatalf("binding = %#v err=%v", binding, err)
		}
	})

	t.Run("qualified member", func(t *testing.T) {
		st, repoRoot, _ := newBoundParentReviewTestStore(t)
		content := "package review\ntype First struct { Shared int }\ntype Second struct { Shared int }\n"
		if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot := currentReviewSnapshotForContract(t, repoRoot)
		if err := recordBoundReviewForContract(st, snapshot, "review.go:First.Shared"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("changed whole diff", func(t *testing.T) {
		st, repoRoot, _ := newBoundParentReviewTestStore(t)
		if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar changed = true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot := currentReviewSnapshotForContract(t, repoRoot)
		if err := recordBoundReviewForContract(st, snapshot, "review.go:@diff"); err != nil {
			t.Fatal(err)
		}
	})
}

func recordBoundReviewForContract(st *StateStore, snapshot SnapshotDigest, target string) error {
	return st.RecordSolResultWithReviewSnapshot(packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect the canonical target.",
		Targets:     []string{target},
	}, ParentReviewProducer{Role: string(ReviewerRole), Model: "reviewer"}, snapshot)
}

func currentReviewSnapshotForContract(t *testing.T, repoRoot string) SnapshotDigest {
	t.Helper()
	snapshot, err := CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
}

func assertReviewTargetAdmissionRejected(t *testing.T, st *StateStore, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("review target admission error = %v, want %q", err, want)
	}
	binding, bindingErr := st.CurrentParentReviewBinding()
	if bindingErr != nil {
		t.Fatal(bindingErr)
	}
	if binding != nil {
		t.Fatalf("rejected target persisted a review binding: %#v", binding)
	}
}
