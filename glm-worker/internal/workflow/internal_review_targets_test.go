package workflow

import (
	"errors"
	"reflect"
	"strings"
	"testing"

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

func TestCurrentReviewDiffTargetsUsesActualTaskPaths(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectReviewTargetPaths = func(string, string) ([]string, error) { return []string{"z.go", "a.go", "z.go"}, nil }
	targets, err := w.currentReviewDiffTargets()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go:@diff", "z.go:@diff"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v want %#v", targets, want)
	}
}

func TestCurrentReviewDiffTargetsRejectsUnavailableOrEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		collect func(string, string) ([]string, error)
		want    string
	}{
		{"lookup-failure", func(string, string) ([]string, error) { return nil, errors.New("changed paths unavailable") }, "changed paths unavailable"},
		{"empty", func(string, string) ([]string, error) { return nil, nil }, "no changed paths"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
			w.collectReviewTargetPaths = tc.collect
			if _, err := w.currentReviewDiffTargets(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v want %q", err, tc.want)
			}
		})
	}
}
