package parentaction

import (
	"os"
	"testing"
)

func TestConsumeExpectedRejectsPayloadChangedAfterValidation(t *testing.T) {
	repo := t.TempDir()
	prepared, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\ncontinue\n")
	writePreparedPayload(t, prepared, original)
	peeked, err := Peek(repo, "decision", prepared.Token)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\nchanged\n")
	writePreparedPayload(t, prepared, changed)
	if _, err := ConsumeExpected(repo, "decision", prepared.Token, peeked); err == nil {
		t.Fatal("changed staged payload was consumed after validation")
	}
	if _, err := os.Lstat(prepared.Path); err != nil {
		t.Fatalf("changed staged payload was discarded: %v", err)
	}
	got, err := ConsumeExpected(repo, "decision", prepared.Token, changed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(changed) {
		t.Fatalf("payload = %q want %q", got, changed)
	}
}
