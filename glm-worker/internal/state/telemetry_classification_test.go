package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCountFinalizedTaskCallsUsesCurrentExplicitTaskRecordsOnly(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID := "task-telemetry-classification"

	lines := make([]string, 0, 5)
	for _, record := range []ModelCallLog{
		{Version: ModelCallLogVersion, TaskID: taskID, CallType: CallTypeTask},
		{Version: ModelCallLogVersion, TaskID: taskID, CallType: CallTypeProbe},
		{Version: ModelCallLogVersion, TaskID: taskID, CallType: CallTypeEvent},
		{Version: ModelCallLogVersion, TaskID: taskID},
		{Version: ModelCallLogVersion - 1, TaskID: taskID, CallType: CallTypeTask},
	} {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	writeTelemetryLinesForCoverage(t, st, taskID, lines)

	count, err := st.CountFinalizedTaskCalls(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("finalized task calls = %d, want 1", count)
	}
}

func TestCountFinalizedTaskCallsMissingFileIsZero(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	count, err := st.CountFinalizedTaskCalls("missing-task")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("finalized task calls = %d, want 0", count)
	}
}

func TestCountFinalizedTaskCallsUnreadableTelemetryFails(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID := "broken-task"
	path := st.ModelCallLogPath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CountFinalizedTaskCalls(taskID); err == nil {
		t.Fatal("unreadable telemetry was accepted")
	}
}
