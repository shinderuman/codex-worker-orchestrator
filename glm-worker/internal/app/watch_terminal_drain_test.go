package app

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type watchTerminalDrainWriter struct {
	bytes.Buffer
	liveStarted chan struct{}
	release     chan struct{}
	blocked     bool
}

func newWatchTerminalDrainWriter() *watchTerminalDrainWriter {
	return &watchTerminalDrainWriter{
		liveStarted: make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (w *watchTerminalDrainWriter) Write(p []byte) (int, error) {
	if !w.blocked && bytes.Contains(p, []byte(`"type":"live"`)) {
		w.blocked = true
		close(w.liveStarted)
		<-w.release
	}
	return w.Buffer.Write(p)
}

type watchTerminalDrainResult struct {
	pending  []byte
	terminal bool
	err      error
}

func TestWatchTaskTickDrainsCompletionBeforeTerminalExit(t *testing.T) {
	st, taskID, base := setupVerboseWatchTask(t)
	file, err := os.Open(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	tracker := newWatchToolTracker()
	pending, err := drainTaskEvents(file, io.Discard, nil, tracker.observe)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracker.pendingTools()) != 1 {
		t.Fatalf("initial pending tools = %#v", tracker.pendingTools())
	}

	out := newWatchTerminalDrainWriter()
	opts := watchTestOptions(true, 0, nil)
	opts.statusInterval = 0
	opts.changeInterval = 0
	opts.now = verboseTestClock(base.Add(20 * time.Minute))
	status := watchLiveStatus{st: st, taskID: taskID, stdout: out, tracker: tracker, opts: opts}

	done := make(chan watchTerminalDrainResult, 1)
	go func() {
		nextPending, terminal, tickErr := watchTaskTick(st, taskID, file, st.TaskEventLogPath(taskID), out, pending, &status, opts)
		done <- watchTerminalDrainResult{pending: nextPending, terminal: terminal, err: tickErr}
	}()

	select {
	case <-out.liveStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("watch tick did not reach the pre-terminal live refresh")
	}

	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{
			TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "user",
			Timestamp: base.Add(10*time.Minute + time.Second),
			Blocks: []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_1", DurationMS: 295100}},
		},
	)
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	close(out.release)

	var result watchTerminalDrainResult
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch tick did not finish after terminal transition")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if !result.terminal {
		t.Fatal("watch tick did not report terminal transition")
	}
	if len(result.pending) != 0 {
		t.Fatalf("terminal drain left pending bytes: %q", result.pending)
	}

	rendered := out.String()
	lives := liveEventsFromStream(t, rendered)
	if len(lives) < 2 {
		t.Fatalf("terminal drain did not emit refreshed live state: %s", rendered)
	}
	final := lives[len(lives)-1]
	if len(final.Current) != 0 || final.Last == nil || final.Last.Name != "Bash" || final.Last.DurationMS != 295100 {
		t.Fatalf("terminal live state = %#v", final)
	}
	events := parseWatchEvents(t, rendered)
	liveIndex := watchEventIndex(events, "live")
	exitIndex := watchEventIndex(events, "watch_exit")
	if liveIndex < 0 || exitIndex < 0 || liveIndex > exitIndex {
		t.Fatalf("terminal event ordering = %v", events)
	}
}
