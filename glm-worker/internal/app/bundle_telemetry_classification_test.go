package app

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBundleInFlightModelCallsUsesCanonicalTaskCallClassification(t *testing.T) {
	_, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	task := bundleTask{ID: taskID, Current: true, Stats: stats}

	st.RecordModelCallLog(state.ModelCallLog{TaskID: taskID})
	if got := bundleInFlightModelCalls(st, task); got != 1 {
		t.Fatalf("empty call_type in-flight calls = %d, want 1", got)
	}

	st.RecordModelCallLog(state.ModelCallLog{TaskID: taskID, CallType: state.CallTypeProbe})
	st.RecordModelCallLog(state.ModelCallLog{TaskID: taskID, CallType: state.CallTypeEvent})
	if got := bundleInFlightModelCalls(st, task); got != 1 {
		t.Fatalf("probe/event in-flight calls = %d, want 1", got)
	}

	st.RecordModelCallLog(state.ModelCallLog{TaskID: taskID, CallType: state.CallTypeTask})
	if got := bundleInFlightModelCalls(st, task); got != 0 {
		t.Fatalf("explicit task in-flight calls = %d, want 0", got)
	}
}

func TestBundleInFlightModelCallsUnreadableTelemetryFailsSafe(t *testing.T) {
	_, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.ModelCallLogPath(taskID), []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	task := bundleTask{ID: taskID, Current: true, Stats: stats}
	if got := bundleInFlightModelCalls(st, task); got != stats.ModelCalls {
		t.Fatalf("unreadable telemetry in-flight calls = %d, want %d", got, stats.ModelCalls)
	}
}
