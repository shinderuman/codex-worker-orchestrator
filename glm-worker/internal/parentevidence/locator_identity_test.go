package parentevidence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type sourceIdentityCase struct {
	path     string
	content  string
	targets  []string
	requests []SourceRequest
	locators []string
}

func TestIdenticalSourceBodiesPreserveDistinctPathTargets(t *testing.T) {
	proveDistinctSourceClaims(t, sourceIdentityCase{
		path: "other.go", content: "package review\nvar other = 2\n",
		targets: []string{"review.go:1", "other.go:1"},
		requests: []SourceRequest{
			{Question: "first path", Path: "review.go", LineStart: 1, LineEnd: 1, BudgetBytes: 4096},
			{Question: "second path", Path: "other.go", LineStart: 1, LineEnd: 1, BudgetBytes: 4096},
		},
		locators: []string{"review.go:1-1", "other.go:1-1"},
	})
}

func TestIdenticalSourceBodiesPreserveDistinctRanges(t *testing.T) {
	proveDistinctSourceClaims(t, sourceIdentityCase{
		path: "repeat.txt", content: "same\nsame\n",
		targets: []string{"repeat.txt:1", "repeat.txt:2"},
		requests: []SourceRequest{
			{Question: "first range", Path: "repeat.txt", LineStart: 1, LineEnd: 1, BudgetBytes: 4096},
			{Question: "second range", Path: "repeat.txt", LineStart: 2, LineEnd: 2, BudgetBytes: 4096},
		},
		locators: []string{"repeat.txt:1-1", "repeat.txt:2-2"},
	})
}

func TestDeliveredBodyReferenceCompletesDistinctLocatorCoverageAcrossCalls(t *testing.T) {
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, "other.go"), []byte("package review\nvar other = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openLocatorIdentityReview(t, repoRoot, st, []string{"review.go:1", "other.go:1"})

	projectReviewCoverageManifest(t, repoRoot, st, Manifest{
		Version: ManifestVersion,
		Reason:  "deliver first locator",
		Source: []SourceRequest{{
			Question: "first path", Path: "review.go", LineStart: 1, LineEnd: 1, BudgetBytes: 4096,
		}},
	})
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof != nil || binding.Coverage == nil || len(binding.Coverage.Claims) != 1 {
		t.Fatalf("first partial coverage = %#v", binding)
	}

	projectReviewCoverageManifest(t, repoRoot, st, Manifest{
		Version: ManifestVersion,
		Reason:  "deliver second locator",
		Source: []SourceRequest{{
			Question: "second path", Path: "other.go", LineStart: 1, LineEnd: 1, BudgetBytes: 4096,
		}},
	})
	assertDistinctSourceClaims(t, st, "review.go:1-1", "other.go:1-1")
}

func TestUnknownOrRefinedBodyReferenceIsNotProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		part Part
	}{
		{
			name: "unknown body reference",
			part: Part{
				Kind: "source", Status: PartProjected, Digest: "same", Reason: UnchangedReason, Locator: "other.go:1-1",
				Source: &SourceBody{Path: "other.go", LineStart: 1, LineEnd: 1},
			},
		},
		{
			name: "budget refinement",
			part: Part{
				Kind: "source", Status: PartRefinement, Digest: "same", Reason: UnchangedReason, Locator: "other.go:1-1",
				Source: &SourceBody{Path: "other.go", LineStart: 1, LineEnd: 1},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims, complete := ReviewClaims([]string{"other.go:1"}, []Part{tc.part})
			if complete || len(claims) != 0 {
				t.Fatalf("unresolved body reference became proof: complete=%v claims=%#v", complete, claims)
			}
		})
	}
}

func proveDistinctSourceClaims(t *testing.T, fixture sourceIdentityCase) {
	t.Helper()
	repoRoot, st := newReviewCoverageStore(t)
	if err := os.WriteFile(filepath.Join(repoRoot, fixture.path), []byte(fixture.content), 0o600); err != nil {
		t.Fatal(err)
	}
	openLocatorIdentityReview(t, repoRoot, st, fixture.targets)
	projectReviewCoverageManifest(t, repoRoot, st, Manifest{
		Version: ManifestVersion,
		Reason:  "same body at distinct locator identities",
		Source:  fixture.requests,
	})
	assertDistinctSourceClaims(t, st, fixture.locators...)
}

func openLocatorIdentityReview(t *testing.T, repoRoot string, st *state.StateStore, targets []string) {
	t.Helper()
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := packet.Result{
		Status:      packet.StatusNeedsSolReview,
		Risk:        packet.RiskHigh,
		SolQuestion: "Inspect every target.",
		Targets:     append([]string(nil), targets...),
	}
	digest := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	if err := st.RecordSolResultWithReviewSnapshot(result, state.ParentReviewProducer{Role: string(state.ReviewerRole), Model: "reviewer"}, digest); err != nil {
		t.Fatal(err)
	}
}

func assertDistinctSourceClaims(t *testing.T, st *state.StateStore, locators ...string) {
	t.Helper()
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		t.Fatal(err)
	}
	if binding == nil || binding.Proof == nil {
		t.Fatalf("review proof missing: %#v", binding)
	}
	if len(binding.Proof.Claims) != len(locators) {
		t.Fatalf("claims=%#v, want %d locator-distinct claims", binding.Proof.Claims, len(locators))
	}
	seen := make(map[string]bool, len(binding.Proof.Claims))
	for _, claim := range binding.Proof.Claims {
		if claim.Kind != "source" {
			t.Fatalf("claim kind=%q, want source", claim.Kind)
		}
		seen[claim.Locator] = true
	}
	for _, locator := range locators {
		if !seen[locator] {
			t.Fatalf("proof lost locator %q: %#v", locator, binding.Proof.Claims)
		}
	}
}
