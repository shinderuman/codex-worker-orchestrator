package workflow

import (
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
)

func TestCanonicalInternalReviewTargets(t *testing.T) {
	targets := canonicalReviewTargets([]string{"a.go", "b.go:12-20", "inspect-current-review", "none", "PACKET", "a.go"})
	if len(targets) != 2 || targets[0] != "a.go:@diff" || targets[1] != "b.go:12-20" {
		t.Fatalf("targets = %#v", targets)
	}
	for _, target := range targets {
		if _, _, err := reviewtarget.Parse(target); err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
	}
}

func TestCurrentReviewDiffTargetsFallsBackWithoutBaseline(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, errors.New("baseline unavailable")
	}
	targets := w.currentReviewDiffTargetsOrFallback([]string{"glm-worker/internal/state/snapshot.go:@diff"})
	if len(targets) != 1 || targets[0] != "glm-worker/internal/state/snapshot.go:@diff" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestSyntheticReviewProducersUseCanonicalTargets(t *testing.T) {
	results := []packet.Result{
		snapshotFailClosedResult("review-start", "snapshot mismatch"),
		reportOnlySnapshotFailClosedResult("report-only-end", "snapshot mismatch"),
		nonConvergedResult(packet.Result{Targets: []string{"a.go"}}),
		parentValidationNonConvergedResult(packet.Result{Targets: []string{"a.go"}}),
	}
	for _, result := range results {
		if len(result.Targets) == 0 {
			t.Fatalf("missing targets: %#v", result)
		}
		for _, target := range result.Targets {
			if _, _, err := reviewtarget.Parse(target); err != nil {
				t.Fatalf("target %q: %v", target, err)
			}
		}
	}
}
