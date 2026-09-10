package workflow

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunModelStopsWhenRepeatedConstraintChangesDisplayValue(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("first", "MEDIUM")},
		{structured: implementedPacketWithRisk("second", "INVALID")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" || failure.Attempts != 1 {
		t.Fatalf("same constraint key must be terminal despite changed display value: err=%v failure=%#v", err, failure)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("same constraint key received another correction: calls=%d", len(r.prompts))
	}
	if len(r.readOnlyCalls) != 2 || r.readOnlyCalls[0] || !r.readOnlyCalls[1] {
		t.Fatalf("correction call must be read-only: %#v", r.readOnlyCalls)
	}
	if len(failure.Violations) != 2 {
		t.Fatalf("terminal evidence must retain both observed messages: %#v", failure.Violations)
	}
	assertNoCorrectionRecoveryAction(t, st)
}

func TestPrepareResumeCheckpointKeepsResultCorrectionReadOnly(t *testing.T) {
	st := newStateStoreT(t)
	checkpoint := baseCorrectionCheckpoint()
	checkpoint.ResultCorrection = true
	checkpoint.ReadOnly = true
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	w := newWorkflowT(t, st, &scriptedRunner{})

	resumed, stopped, err := w.prepareResumeCheckpoint(checkpoint, externalFeasibility{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("result correction resume unexpectedly stopped")
	}
	if !resumed.ReadOnly {
		t.Fatal("result correction resume became writable")
	}
}
