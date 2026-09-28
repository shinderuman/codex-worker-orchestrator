package state

import (
	"errors"
	"os"
	"testing"
)

func TestParentReopenClearsFinalizationEvidence(t *testing.T) {
	st := newAcceptedParentCompletionStore(t)
	candidate := saveReopenPublicationState(t, st)
	if err := st.SaveFinalizationEvidence(NewFinalizationEvidence(candidate.TaskID, "go-test", "run-final", candidate.SnapshotID)); err != nil {
		t.Fatal(err)
	}
	recordReopenFinding(t, st)
	if err := st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadFinalizationEvidence(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reopened task retained finalization evidence: %v", err)
	}
}
