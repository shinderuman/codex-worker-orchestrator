package workflow

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInternalReviewProducersDoNotEmitWithoutActualTaskTargets(t *testing.T) {
	failure := packet.Result{Status: packet.StatusFixRequired, Risk: packet.RiskHigh, Summary: "failure", RequirementCoverage: "covered", Invariants: "preserved", TestEvidence: "evidence", Issues: "issue", ResidualRisk: "risk"}
	producers := []struct {
		name string
		run  func(*Workflow) error
	}{
		{"quality-surface", func(w *Workflow) error { return w.failClosedQualitySurface("worker-new", "changed", nil) }},
		{"snapshot", func(w *Workflow) error {
			return w.failClosedSnapshot(state.SnapshotStageReviewStart, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil)
		}},
		{"report-only-snapshot", func(w *Workflow) error {
			return w.failClosedReportOnlySnapshot(state.SnapshotStageReportOnlyEnd, state.GitSnapshot{}, state.GitSnapshot{}, "mismatch", nil)
		}},
		{"non-convergence", func(w *Workflow) error { _, err := w.nonConvergedResult(failure); return err }},
		{"parent-validation", func(w *Workflow) error { _, err := w.parentValidationNonConvergedResult(failure); return err }},
	}
	modes := []struct {
		name    string
		collect func(string, string) ([]string, error)
	}{
		{"lookup-failure", func(string, string) ([]string, error) { return nil, errors.New("changed paths unavailable") }},
		{"empty", func(string, string) ([]string, error) { return nil, nil }},
	}
	for _, producer := range producers {
		for _, mode := range modes {
			t.Run(producer.name+"/"+mode.name, func(t *testing.T) {
				st := newStateStoreT(t)
				var out bytes.Buffer
				w := newWorkflowTWithOutput(t, st, &scriptedRunner{}, &out)
				w.collectChangedPaths = mode.collect
				before := st.TaskStatus()
				if err := producer.run(w); err == nil {
					t.Fatal("missing semantic targets must return an error")
				}
				if out.Len() != 0 {
					t.Fatalf("producer emitted a packet without semantic targets: %s", out.String())
				}
				if st.TaskStatus() != before {
					t.Fatalf("producer committed state without semantic targets: before=%s after=%s", before, st.TaskStatus())
				}
			})
		}
	}
}

func TestQualitySurfaceTargetsRemainQualitySurfaceScoped(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/workflow/workflow.go", "commentlint", "IMPLEMENTATION_RULES.md"}, nil
	}
	targets, err := w.currentQualitySurfaceReviewTargets()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if strings.HasPrefix(target, "glm-worker/internal/workflow/workflow.go:") {
			t.Fatalf("unrelated implementation target leaked into quality-surface review: %#v", targets)
		}
	}
	if len(targets) == 0 {
		t.Fatal("expected at least one quality-surface target")
	}
}

func TestNonConvergenceAndParentValidationPreserveCanonicalTargets(t *testing.T) {
	input := packet.Result{Targets: []string{"pkg/reviewer.go:12-20"}}
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, errors.New("collector must not be consulted when canonical targets exist")
	}

	nonConverged, err := w.nonConvergedResult(input)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := w.parentValidationNonConvergedResult(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pkg/reviewer.go:12-20"}
	if !reflect.DeepEqual(nonConverged.Targets, want) {
		t.Fatalf("non-convergence targets = %#v want %#v", nonConverged.Targets, want)
	}
	if !reflect.DeepEqual(parent.Targets, want) {
		t.Fatalf("parent-validation targets = %#v want %#v", parent.Targets, want)
	}
}

func TestNonConvergenceAndParentValidationFallbackToActualTaskDiff(t *testing.T) {
	input := packet.Result{}
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"pkg/z.go", "pkg/a.go"}, nil
	}

	nonConverged, err := w.nonConvergedResult(input)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := w.parentValidationNonConvergedResult(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pkg/a.go:@diff", "pkg/z.go:@diff"}
	if !reflect.DeepEqual(nonConverged.Targets, want) {
		t.Fatalf("non-convergence targets = %#v want %#v", nonConverged.Targets, want)
	}
	if !reflect.DeepEqual(parent.Targets, want) {
		t.Fatalf("parent-validation targets = %#v want %#v", parent.Targets, want)
	}
}
