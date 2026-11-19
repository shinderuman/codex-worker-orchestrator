package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

func TestStatusEventReadIncompleteDoesNotProjectStaleEvent(t *testing.T) {
	t.Parallel()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	writeTaskEventLines(t, st, taskID, state.TaskEventRecord{
		TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new",
		ModelAlias: "opus", Seq: 1, Kind: "system", Timestamp: time.Now().UTC(),
	})

	before := executeStatusOutput(t, cfg)
	statusString(t, "event_log", before.EventLog, "ok")
	if before.LastEvent == nil || before.LastEvent.Seq != 1 {
		t.Fatalf("initial last_event = %#v", before.LastEvent)
	}

	// The scanner must not return seq=1 as current when a too-long seq=2
	// prevents it from reaching a valid seq=3.
	path := st.TaskEventLogPath(taskID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(strings.Repeat("x", 4*1024*1024+1) + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write oversized line: write=%v close=%v", writeErr, closeErr)
	}
	writeTaskEventLines(t, st, taskID, state.TaskEventRecord{
		TaskID: taskID, CallID: "call-1", Role: "reviewer", Phase: "reviewer-1",
		ModelAlias: "haiku", Seq: 3, Kind: "system", Timestamp: time.Now().UTC(),
	})

	output := executeStatusOutput(t, cfg)
	statusString(t, "event_log", output.EventLog, taskview.StatusPartial)
	if output.LastEvent != nil || output.LastEventAgeMS != nil {
		t.Fatalf("partial event log exposed stale event: %#v", output.LastEvent)
	}
	if output.CurrentPhase != nil || output.CurrentRole != nil || output.CurrentModel != nil {
		t.Fatalf("partial event log exposed unverified current call: %#v", output)
	}
	if output.Progress == nil || output.Progress.Status != "indeterminate" ||
		output.Progress.Precision != "unavailable" || output.Progress.PhaseStage != "unknown" ||
		output.Progress.Reason != "event-log-read-incomplete" {
		t.Fatalf("partial event log progress = %#v", output.Progress)
	}
}

func TestStatusEventLogOpenFailureIsUnknown(t *testing.T) {
	t.Parallel()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	// A regular file in the parent position produces an open error rather
	// than a missing log, and must not silently fall back to a checkpoint.
	eventsDir := filepath.Dir(st.TaskEventLogPath(taskID))
	if err := os.RemoveAll(eventsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventsDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	statusString(t, "event_log", output.EventLog, taskview.StatusUnreadable)
	if output.LastEvent != nil || output.CurrentPhase != nil || output.CurrentRole != nil || output.CurrentModel != nil {
		t.Fatalf("unreadable event log exposed an event/current phase: %#v", output)
	}
	if output.Progress == nil || output.Progress.Status != "indeterminate" ||
		output.Progress.Reason != "event-log-unreadable" {
		t.Fatalf("unreadable event log progress = %#v", output.Progress)
	}
}

func TestStatusEventLogMissingRemainsCheckpointFallback(t *testing.T) {
	t.Parallel()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	output := executeStatusOutput(t, cfg)
	statusString(t, "event_log", output.EventLog, taskview.StatusNone)
	if output.LastEvent != nil || output.CurrentPhase != nil {
		t.Fatalf("missing event log unexpectedly yielded event: %#v", output)
	}
}
