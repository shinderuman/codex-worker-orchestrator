package workflow

import "testing"

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
	if len(failure.Violations) != 2 {
		t.Fatalf("terminal evidence must retain both observed messages: %#v", failure.Violations)
	}
	assertNoCorrectionRecoveryAction(t, st)
}
