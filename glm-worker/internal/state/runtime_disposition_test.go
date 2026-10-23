package state

import (
	"testing"
)

func TestRuntimeDispositionPreservesFailureAndRejectsReplacement(t *testing.T) {
	st := newParentActionTestStore(t)
	id, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	record := RuntimeDisposition{Version: 1, TaskID: id, TaskPath: "IMPLEMENTATION_TASKS/task.md", TransitionID: "transition", EvidenceDigest: "archive", Reason: "explicit termination", PreviousStatus: TaskStatusWaitingDecision}
	if err := st.RecordRuntimeDisposition(record); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusUserTerminated || !st.Exists("pending-decision") {
		t.Fatal("disposition fabricated completion or erased pending state")
	}
	if _, admitted, err := st.AdmitNewTask(); err != nil || !admitted {
		t.Fatalf("terminal admission: %v", err)
	}
	changed := record
	changed.Reason = "different reason"
	if err := st.RecordRuntimeDisposition(changed); err == nil {
		t.Fatal("termination evidence replaced")
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(RuntimeDispositionFile) || st.TaskStatus() != TaskStatusActive {
		t.Fatal("new task inherited disposition")
	}
}
