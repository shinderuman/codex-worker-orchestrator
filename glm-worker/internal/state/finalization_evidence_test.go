package state

import (
	"errors"
	"os"
	"testing"
)

func TestFinalizationEvidenceRoundTripAndFreshTaskClear(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	evidence := NewFinalizationEvidence(taskID, "go-test", "run-final", "snapshot-final")
	if err := st.SaveFinalizationEvidence(evidence); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadFinalizationEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != evidence {
		t.Fatalf("loaded evidence = %#v want %#v", loaded, evidence)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadFinalizationEvidence(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prior finalization evidence survived fresh task: %v", err)
	}
}
