package workflow

import (
	"bytes"
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInternalReviewProducersDoNotEmitWithoutActualTaskTargets(t *testing.T) {
	failure := packet.Result{Status: packet.StatusFixRequired, Risk: packet.RiskHigh, Summary: "failure", RequirementCoverage: "covered", Invariants: "preserved", TestEvidence: "evidence", Issues: "issue", ResidualRisk: "risk", Targets: []string{"syntactic.go:@diff"}}
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
				w.collectReviewTargetPaths = mode.collect
				before := st.TaskStatus()
				if err := producer.run(w); err == nil {
					t.Fatal("missing actual task targets must return an error")
				}
				if out.Len() != 0 {
					t.Fatalf("producer emitted a packet without actual task targets: %s", out.String())
				}
				if st.TaskStatus() != before {
					t.Fatalf("producer committed state without actual targets: before=%s after=%s", before, st.TaskStatus())
				}
			})
		}
	}
}
