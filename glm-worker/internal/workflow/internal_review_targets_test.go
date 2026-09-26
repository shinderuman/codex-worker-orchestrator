package workflow

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCurrentReviewDiffTargetsUsesActualTaskPaths(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectReviewTargetPaths = func(string, string) ([]string, error) {
		return []string{"z.go", "commentlint", "a.go", "z.go"}, nil
	}
	targets, err := w.currentReviewDiffTargets()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go:@diff", "commentlint:@diff", "z.go:@diff"}
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
