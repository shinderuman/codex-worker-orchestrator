package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// TestReviewAdmissionRequiresReachableQualityGate exercises the real worker to
// reviewer entrypoint. A disconnected helper containing the correct calls
// cannot make these cases pass without the gate on the executable path.
func TestReviewAdmissionRequiresReachableQualityGate(t *testing.T) {
	t.Parallel()
	invalid := harnesslint.Report{}
	failWithoutViolations := harnesslint.Report{Status: "fail"}
	passWithViolations := harnesslint.Report{
		Status: "pass",
		Violations: []harnesslint.Violation{{Rule: "test", Path: "tracked.md", Line: 1}},
	}
	for _, tc := range []struct {
		name       string
		report     harnesslint.Report
		gateErr    error
		mutate     bool
		wantReview bool
	}{
		{name: "accepted pass", report: harnesslint.Report{Status: "pass"}, wantReview: true},
		{name: "missing gate result", report: invalid},
		{name: "failed gate execution", gateErr: errors.New("quality tool unavailable")},
		{name: "fail without violation evidence", report: failWithoutViolations},
		{name: "pass with contradictory violations", report: passWithViolations},
		{name: "stale worker end during gate", report: harnesslint.Report{Status: "pass"}, mutate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRetentionGitRepo(t)
			st := newGitStateStoreT(t, repo)
			r := &scriptedRunner{steps: []runnerStep{
				{structured: implementedPacket("implemented")},
				{structured: passPacket()},
			}}
			w := newGitWorkflowT(t, st, r, repo)
			w.collectChangedPaths = func(string, string) ([]string, error) {
				return []string{"tracked.md"}, nil
			}
			calls := 0
			w.qualityGate = func(root string) (harnesslint.Report, error) {
				calls++
				if !reflect.DeepEqual(r.phases, []string{"worker-new"}) {
					t.Fatalf("gate must run after worker but before reviewer: phases=%v", r.phases)
				}
				workerEnd, err := st.LoadWorkerEndSnapshot()
				if err != nil || workerEnd.Head == "" {
					t.Fatalf("gate must consume a persisted worker-end snapshot: %#v err=%v", workerEnd, err)
				}
				if tc.mutate {
					if err := os.WriteFile(filepath.Join(root, "tracked.md"), []byte("changed during quality gate\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return tc.report, tc.gateErr
			}
			err := w.ExecuteNewTask("request")
			if calls != 1 {
				t.Fatalf("gate calls=%d, want exactly one fix-first validation", calls)
			}
			if tc.wantReview {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(r.phases, []string{"worker-new", "reviewer-1"}) {
					t.Fatalf("reviewer must follow the accepted gate: phases=%v", r.phases)
				}
				if st.TaskStatus() != state.TaskStatusComplete {
					t.Fatalf("task status=%s, want complete", st.TaskStatus())
				}
				workerEnd, err := st.LoadWorkerEndSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				reviewStart, err := st.LoadReviewStartSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				if workerEnd.Head != reviewStart.Head ||
					workerEnd.IndexDigest != reviewStart.IndexDigest ||
					workerEnd.WorktreeDigest != reviewStart.WorktreeDigest {
					t.Fatalf("review admitted a different snapshot: worker=%+v review=%+v", workerEnd, reviewStart)
				}
				return
			}
			if tc.mutate {
				// A clean gate report cannot authorize a changed review input.
				// The review-start snapshot guard rejects it without a recoverable
				// quality-tool failure (and without invoking the reviewer).
				if err != nil {
					t.Fatal(err)
				}
				if len(r.phases) != 1 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
					t.Fatalf("stale review input was admitted: phases=%v status=%s", r.phases, st.TaskStatus())
				}
				comparison, err := st.LoadSnapshotComparison()
				if err != nil {
					t.Fatal(err)
				}
				if comparison.Stage != state.SnapshotStageReviewStart || comparison.Matched {
					t.Fatalf("stale review-start snapshot was not rejected: %+v", comparison)
				}
				return
			}
			var stopped *QualityGateRecoverableError
			if !errors.As(err, &stopped) {
				t.Fatalf("missing/failing/stale gate must stop recoverably: %v", err)
			}
			if len(r.phases) != 1 || r.phases[0] != "worker-new" {
				t.Fatalf("reviewer ran without accepted gate: phases=%v", r.phases)
			}
			if st.TaskStatus() != state.TaskStatusQualityGateRecoverable {
				t.Fatalf("status=%s, want quality-gate-recoverable", st.TaskStatus())
			}
			if !stopped.ResultSaved {
				t.Fatalf("worker result was lost on gate stop: %+v", stopped)
			}
			if tc.gateErr == nil && !tc.mutate && !strings.Contains(stopped.Failure, "invalid machine quality gate report") {
				t.Fatalf("invalid gate failure reason=%q", stopped.Failure)
			}
		})
	}
}

// A saved worker result does not bypass the gate when quality tools recover:
// resume must run a fresh gate before it can invoke a reviewer.
func TestReviewAdmissionResumeRechecksGateBeforeReviewer(t *testing.T) {
	t.Parallel()
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopQualityGateWithVersionMismatch(t, st, repo)

	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newGitWorkflowT(t, st, r, repo)
	w.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	calls := 0
	w.qualityGate = func(string) (harnesslint.Report, error) {
		calls++
		if len(r.phases) != 0 {
			t.Fatalf("reviewer was invoked before resume gate: phases=%v", r.phases)
		}
		return harnesslint.Report{Status: "pass"}, nil
	}
	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(r.phases, []string{"reviewer-1"}) {
		t.Fatalf("resume must revalidate exactly once, without recalling worker: gates=%d phases=%v", calls, r.phases)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("resume status=%s", st.TaskStatus())
	}
}
