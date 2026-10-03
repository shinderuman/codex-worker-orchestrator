package parentevidence

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type failingReviewCoverageWriter struct{}

func (failingReviewCoverageWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout render failure")
}

func TestReviewCoverageSurvivesStateStoreRestart(t *testing.T) {
	repoRoot := t.TempDir()
	gitReviewCoverageCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar first = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "other.go"), []byte("package review\nvar second = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitReviewCoverageCommand(t, repoRoot, "add", "review.go", "other.go")
	gitReviewCoverageCommand(t, repoRoot, "-c", "user.name=coverage restart", "-c", "user.email=coverage-restart@example.invalid", "commit", "-q", "-m", "seed")

	cfg := config.AppConfig{StateBase: t.TempDir(), RepoHash: "review-coverage-restart", RepoRoot: repoRoot}
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
	openLocatorIdentityReview(t, repoRoot, st, []string{"review.go:2", "other.go:2"})
	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("first", "review.go", 2, 4096))

	restarted, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	projectReviewCoverageManifest(t, repoRoot, restarted, oneSourceManifest("second", "other.go", 2, 4096))
	assertDistinctSourceClaims(t, restarted, "review.go:2-2", "other.go:2-2")
}

func TestReviewCoverageLeaseRotationDropsPriorPartialCoverage(t *testing.T) {
	repoRoot, st := twoTargetCoverageStore(t)
	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("first", "review.go", 2, 4096))
	if err := st.RotateParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("second", "other.go", 2, 4096))

	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof != nil || binding.Coverage == nil || len(binding.Coverage.Claims) != 1 || binding.Coverage.Claims[0].Target != "other.go:2" {
		t.Fatalf("lease rotation retained stale coverage: %#v", binding)
	}
	ready, err := st.ParentReviewAcceptReady()
	if err != nil || ready {
		t.Fatalf("accept readiness after lease rotation = %v err=%v", ready, err)
	}
}

func TestReviewCoverageSnapshotChangeRejectsAccumulation(t *testing.T) {
	repoRoot, st := twoTargetCoverageStore(t)
	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("first", "review.go", 2, 4096))
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("package review\nvar first = 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	projector := reviewCoverageProjector(t, repoRoot, st, oneSourceManifest("second", "other.go", 2, 4096))
	if err := projector.Commit(&bytes.Buffer{}, "snapshot changed"); err == nil {
		t.Fatal("snapshot change unexpectedly allowed coverage accumulation")
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof != nil || binding.Coverage == nil || len(binding.Coverage.Claims) != 1 {
		t.Fatalf("snapshot failure changed prior coverage: %#v", binding)
	}
}

func TestReviewCoverageStdoutFailureDoesNotPersistCoverage(t *testing.T) {
	repoRoot, st := twoTargetCoverageStore(t)
	projector := reviewCoverageProjector(t, repoRoot, st, oneSourceManifest("first", "review.go", 2, 4096))
	if err := projector.Commit(failingReviewCoverageWriter{}, "stdout failure"); err == nil {
		t.Fatal("stdout failure unexpectedly succeeded")
	}
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Coverage != nil || binding.Proof != nil {
		t.Fatalf("stdout failure persisted review evidence: %#v", binding)
	}
}

func TestReviewCoverageBudgetRefinementDoesNotCount(t *testing.T) {
	repoRoot, st := twoTargetCoverageStore(t)
	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("too small", "review.go", 2, 1))
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Coverage != nil || binding.Proof != nil {
		t.Fatalf("budget-refined source became coverage: %#v", binding)
	}
}

func TestReviewCoverageCompletesAfterTotalBudgetForcesSplitDelivery(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	large := strings.Repeat("x", 60*1024)
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "other.go"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	openLocatorIdentityReview(t, repoRoot, st, []string{"review.go:1", "other.go:1"})

	projectReviewCoverageManifest(t, repoRoot, st, Manifest{
		Version: ManifestVersion,
		Reason:  "combined output exceeds total budget",
		Source: []SourceRequest{
			{Question: "first large target", Path: "review.go", LineStart: 1, LineEnd: 1, BudgetBytes: MaxBudgetBytes},
			{Question: "second large target", Path: "other.go", LineStart: 1, LineEnd: 1, BudgetBytes: MaxBudgetBytes},
		},
	})
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof != nil || binding.Coverage == nil || len(binding.Coverage.Claims) != 1 {
		t.Fatalf("combined over-budget projection did not preserve exactly one delivered target: %#v", binding)
	}

	projectReviewCoverageManifest(t, repoRoot, st, oneSourceManifest("second bounded delivery", "other.go", 1, MaxBudgetBytes))
	assertDistinctSourceClaims(t, st, "review.go:1-1", "other.go:1-1")
}

func TestConcurrentReviewCoverageCallsUnionUnderLedgerLock(t *testing.T) {
	repoRoot, st := twoTargetCoverageStore(t)
	first := reviewCoverageProjector(t, repoRoot, st, oneSourceManifest("first", "review.go", 2, 4096))
	second := reviewCoverageProjector(t, repoRoot, st, oneSourceManifest("second", "other.go", 2, 4096))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, projector := range []*Projector{first, second} {
		wg.Add(1)
		go func(projector *Projector) {
			defer wg.Done()
			errs <- projector.Commit(&bytes.Buffer{}, "concurrent coverage")
		}(projector)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertDistinctSourceClaims(t, st, "review.go:2-2", "other.go:2-2")
}

func twoTargetCoverageStore(t *testing.T) (string, *state.StateStore) {
	t.Helper()
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "other.go"), []byte("package review\nvar second = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openLocatorIdentityReview(t, repoRoot, st, []string{"review.go:2", "other.go:2"})
	return repoRoot, st
}

func oneSourceManifest(question, path string, line, budget int) Manifest {
	return Manifest{
		Version: ManifestVersion,
		Reason:  question,
		Source: []SourceRequest{{
			Question: question, Path: path, LineStart: line, LineEnd: line, BudgetBytes: budget,
		}},
	}
}

func reviewCoverageProjector(t *testing.T, repoRoot string, st *state.StateStore, manifest Manifest) *Projector {
	t.Helper()
	ownerCallID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	projector := NewProjector(repoRoot, st, ownerCallID, Providers{})
	projector.Project(manifest)
	return projector
}
