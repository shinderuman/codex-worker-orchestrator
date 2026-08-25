package app

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func watchNonENOENTError(op string, path string) error {
	return &fs.PathError{Op: op, Path: path, Err: fs.ErrPermission}
}

func TestWatchInitialOpenNonENOENTFailsClosed(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)
	path := st.TaskEventLogPath(taskID)

	opts := watchTestOptions(false, time.Millisecond, nil)
	opts.openEventLog = func(opened string) (*os.File, error) {
		if opened != path {
			t.Errorf("open対象 = %q want %q", opened, path)
		}
		return nil, watchNonENOENTError("open", opened)
	}
	out := &bytes.Buffer{}
	err := printWatch(st, out, opts)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("非ENOENT open失敗がerrorになっていません: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("errorにevent log pathのcontextがありません: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("非ENOENT open失敗時にstdoutへ出力があります: %q", out.String())
	}
}

func TestWatchFollowStatNonENOENTFailsAfterStreaming(t *testing.T) {
	st, _ := watchTestStore(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", Kind: "system", Subtype: "init"},
	)
	path := st.TaskEventLogPath(taskID)

	opts := watchTestOptions(false, time.Millisecond, nil)
	opts.statEventLog = func(statted string) (os.FileInfo, error) {
		return nil, watchNonENOENTError("stat", statted)
	}
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- printWatch(st, out, opts)
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchがstat失敗で終了しません")
	}
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("非ENOENT stat失敗がerrorになっていません: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("errorにevent log pathのcontextがありません: %v", err)
	}

	events := parseWatchEvents(t, out.String())
	if watchString(t, requireWatchEvent(t, events, "watch_start"), "event_log_status") != "following" {
		t.Fatalf("watch_start = %v", events)
	}
	if watchString(t, events[1], "kind") != "system" {
		t.Fatalf("保存済みeventが流れていません: %v", events)
	}
	if watchEventIndex(events, "event_log_status") >= 0 {
		t.Fatalf("I/O失敗がremoved status eventへ偽装されています: %v", events)
	}
	if watchEventIndex(events, "watch_exit") >= 0 {
		t.Fatalf("I/O失敗時にwatch_exitが出ています: %v", events)
	}
}
