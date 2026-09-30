package state

import (
	"errors"
	"testing"
)

func TestStartNewTaskClearsGuardRepairRecord(t *testing.T) {
	st := newGuardRepairStateStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	record := guardRepairRecordForTest()
	record.TaskID = taskID
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}

	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadGuardRepairRecord(); !errors.Is(err, ErrNoGuardRepairRecord) {
		t.Fatalf("new task retained guard repair record: %v", err)
	}
}

func TestResetClearsGuardRepairRecord(t *testing.T) {
	st := newGuardRepairStateStore(t)
	record := guardRepairRecordForTest()
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}

	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadGuardRepairRecord(); !errors.Is(err, ErrNoGuardRepairRecord) {
		t.Fatalf("reset retained guard repair record: %v", err)
	}
}
