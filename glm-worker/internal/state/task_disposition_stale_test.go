package state

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestResetWithDispositionRecoversLegacyPartialResetProvenance(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	// Legacy Reset archived stats before deleting task-bound state. Simulate a
	// partial deletion that lost task.id and task.status but left parent review
	// state and the archived stats as machine-readable provenance.
	st.ArchiveCurrentStats()
	if err := st.Remove("task.id", "task.status"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CurrentTaskStats(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy partial reset simulation left current stats: %v", err)
	}
	if got := st.TaskStatus(); got != TaskStatusNone {
		t.Fatalf("legacy partial reset simulation left task status %q", got)
	}

	if err := st.ValidateResetRequest(""); err == nil || !strings.Contains(err.Error(), "explicit disposition") {
		t.Fatalf("orphaned unfinished task accepted generic reset: %v", err)
	}
	if err := st.ValidateResetRequest(string(TaskDispositionAbandon)); err != nil {
		t.Fatalf("orphaned task provenance did not admit explicit abandon: %v", err)
	}
	disposition, err := st.ResetWithDisposition(string(TaskDispositionAbandon))
	if err != nil {
		t.Fatal(err)
	}
	if disposition != TaskDispositionAbandon {
		t.Fatalf("disposition = %q", disposition)
	}

	record, err := st.CurrentTaskDisposition()
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != taskID || record.FromStatus != string(TaskStatusAwaitingParentCompletion) || record.Disposition != TaskDispositionAbandon {
		t.Fatalf("recovered disposition record = %#v", record)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("recovered stale reset provenance is not admissible: %v", err)
	}
}
