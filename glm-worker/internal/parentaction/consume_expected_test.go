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

func TestConsumeExpectedOneShotRejectsSecondRead(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	prepared, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\ncontinue\n")
	writePreparedPayload(t, prepared, payload)
	peeked, err := Peek(repo, "decision", prepared.Token)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ConsumeExpected(repo, "decision", prepared.Token, peeked)
	if err != nil || string(first) != string(payload) {
		t.Fatalf("first consume: %q %v", first, err)
	}
	if _, err := ConsumeExpected(repo, "decision", prepared.Token, peeked); err == nil {
		t.Fatal("one-shot payload was read a second time")
	}
	if _, err := os.Lstat(prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("consumed payload was left readable: %v", err)
	}
}
