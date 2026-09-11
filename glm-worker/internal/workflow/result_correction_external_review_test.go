package workflow

import (
	"testing"
)

func TestRunModelInvalidCorrectionResponseIsTerminal(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: "{"},
		{structured: implementedPacket("must-not-run")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "invalid_response" {
		t.Fatalf("invalid correction response terminal failureを期待: err=%v failure=%#v", err, failure)
	}
	assertTerminalCorrectionState(t, w, "invalid_response")
	assertNoCorrectionRecoveryAction(t, st)
	if len(r.prompts) != 2 {
		t.Fatalf("invalid correction response後のmodel calls = %d want 2", len(r.prompts))
	}

	if _, err := w.runModel(baseCorrectionCheckpoint()); err == nil {
		t.Fatal("terminal invalid-response latchがfresh model callを許可しました")
	}
	if len(r.prompts) != 2 {
		t.Fatalf("terminal invalid-response latch後にmodel callが実行されました: %d", len(r.prompts))
	}
}
