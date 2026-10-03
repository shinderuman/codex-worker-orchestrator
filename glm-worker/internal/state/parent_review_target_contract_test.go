package state

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestParentReviewBindingRejectsNonCanonicalTargetBeforePersistence(t *testing.T) {
	st, _, snapshot := newBoundParentReviewTestStore(t)
	err := recordBoundReviewForContract(st, snapshot, "review.go:AppendRecord-acquireRegistryLock")
	assertReviewTargetAdmissionRejected(t, st, err, "correction=")
}

func TestReviewTargetProofAddressabilityRejectsUnavailableInstances(t *testing.T) {
	t.Run("unchanged diff", func(t *testing.T) {
		_, repoRoot, _ := newBoundParentReviewTestStore(t)
		assertProofAddressabilityRejected(t, repoRoot, "review.go:@diff", "[diff-absent]")
	})

	t.Run("untracked diff", func(t *testing.T) {
		_, repoRoot, _ := newBoundParentReviewTestStore(t)
		if err := os.WriteFile(filepath.Join(repoRoot, "new.go"), []byte("package review\nfunc NewAPI() {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertProofAddressabilityRejected(t, repoRoot, "new.go:@diff", "[diff-absent]")
	})

	t.Run("local variable is not top-level symbol", func(t *testing.T) {
		_, repoRoot, _ := newBoundParentReviewTestStore(t)
		content := "package review\nfunc Caller() {\n\tLocal := 1\n\t_ = Local\n}\n"
		if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		assertProofAddressabilityRejected(t, repoRoot, "review.go:Local", "[symbol-not-declared]")
	})

	t.Run("oversized symbol declaration", func(t *testing.T) {
		_, repoRoot, _ := newBoundParentReviewTestStore(t)
		var source strings.Builder
		source.WriteString("package review\nfunc Huge() {\n")
		for range reviewtarget.MaxSourceProofLines {
			source.WriteString("\t_ = 0\n")
		}
		source.WriteString("}\n")
		if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte(source.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		assertProofAddressabilityRejected(t, repoRoot, "review.go:Huge", "[symbol-source-too-large]")
	})

	t.Run("oversized numeric range", func(t *testing.T) {
		_, repoRoot, _ := newBoundParentReviewTestStore(t)
		target := "review.go:1-" + strconv.Itoa(reviewtarget.MaxSourceProofLines+1)
		assertProofAddressabilityRejected(t, repoRoot, target, "[line-range-too-large]")
	})
}

func TestParentReviewBindingAdmitsCanonicalTargets(t *testing.T) {
	for _, target := range []string{"review.go:target", "review.go:1", "review.go:1-2"} {
		t.Run(target, func(t *testing.T) {
			st, _, snapshot := newBoundParentReviewTestStore(t)
			if err := recordBoundReviewForContract(st, snapshot, target); err != nil {
				t.Fatal(err)
			}
			binding, err := st.CurrentParentReviewBinding()
			if err != nil || binding == nil || len(binding.Targets) != 1 || binding.Targets[0] != target {
				t.Fatalf("binding = %#v err=%v", binding, err)
			}
		})
	}
}

func assertProofAddressabilityRejected(t *testing.T, repoRoot, raw, want string) {
	t.Helper()
	target, err := reviewtarget.ParseTarget(raw)
	if err != nil {
		t.Fatal(err)
	}
	err = reviewtarget.ValidateProofAddressable(repoRoot, target)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("proof addressability error = %v, want %q", err, want)
	}
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
