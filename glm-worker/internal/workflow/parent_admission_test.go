package workflow

import (
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type admissionTestRunner struct {
	calls int
}

func (r *admissionTestRunner) Run(state.SessionRole, string, string, bool, string, string, string) (runner.RunResult, error) {
	r.calls++
	return runner.RunResult{}, nil
}

func (r *admissionTestRunner) Probe(string) (runner.ProbeResult, error) {
	r.calls++
	return runner.ProbeResult{}, nil
}

func TestDirectWorkflowDecisionAdmissionRejectsBeforeModelOrMutation(t *testing.T) {
	cfg, st := newAdmissionTestWorkflowState(t)
	r := &admissionTestRunner{}
	w := NewWorkflow(cfg, st, r, io.Discard)
	before := st.TaskStatus()

	err := w.ExecuteDecision("continue")
	if err == nil || err.Error() != "no pending Sol decision for this repository" {
		t.Fatalf("ExecuteDecision error = %v", err)
	}
	if r.calls != 0 {
		t.Fatalf("model calls = %d, want 0", r.calls)
	}
	if got := st.TaskStatus(); got != before {
		t.Fatalf("task status changed from %s to %s", before, got)
	}
}

func TestDirectWorkflowAdmissionFailsClosedOnLifecycleInconsistency(t *testing.T) {
	cfg, st := newAdmissionTestWorkflowState(t)
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	r := &admissionTestRunner{}
	w := NewWorkflow(cfg, st, r, io.Discard)

	err := w.ExecuteDecision("continue")
	if err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency") {
		t.Fatalf("ExecuteDecision error = %v", err)
	}
	if r.calls != 0 {
		t.Fatalf("model calls = %d, want 0", r.calls)
	}
	if got := st.TaskStatus(); got != state.TaskStatusWaitingDecision {
		t.Fatalf("task status = %s", got)
	}
	if st.Exists("pending-decision") {
		t.Fatal("rejected admission created pending-decision")
	}
}

func newAdmissionTestWorkflowState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "parent-admission-test",
		RepoRoot:  t.TempDir(),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}
