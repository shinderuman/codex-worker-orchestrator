package taskview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func eventLogLine(t *testing.T, taskID string, seq int) string {
	t.Helper()
	data, err := json.Marshal(state.TaskEventRecord{
		Version: 1, TaskID: taskID, CallID: "call-1",
		Seq: seq, Role: "worker", Phase: "worker-new",
		Kind: "system", Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

func TestReadLastTaskEventCompleteness(t *testing.T) {
	t.Parallel()
	first := eventLogLine(t, "task-1", 1)
	third := eventLogLine(t, "task-1", 3)
	foreign := eventLogLine(t, "task-other", 3)
	tooLong := strings.Repeat("x", 4*1024*1024+1) + "\n"
	for _, tc := range []struct {
		name    string
		content string
		wantSeq int
		partial bool
	}{
		{name: "normal tail", content: first + third, wantSeq: 3},
		{name: "normal tail without newline", content: first + strings.TrimSuffix(third, "\n"), wantSeq: 3},
		{name: "malformed line then valid tail", content: first + "{invalid\n" + third, wantSeq: 3},
		{name: "malformed tail", content: first + "{invalid", partial: true},
		{name: "foreign tail", content: first + foreign, partial: true},
		{name: "overlong middle then valid tail", content: first + tooLong + third, partial: true},
		{name: "overlong tail", content: first + tooLong, partial: true},
		{name: "empty", content: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			last, found, err := readLastTaskEvent(path, "task-1")
			if tc.partial {
				if !errors.Is(err, ErrEventLogReadIncomplete) || found || last.Seq != 0 {
					t.Fatalf("incomplete read: last=%#v found=%v err=%v", last, found, err)
				}
				return
			}
			if err != nil || found != (tc.wantSeq != 0) || last.Seq != tc.wantSeq {
				t.Fatalf("complete read: last=%#v found=%v err=%v; want seq=%d", last, found, err, tc.wantSeq)
			}
		})
	}
}

func TestReadLastTaskEventReadErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if last, found, err := readLastTaskEvent(filepath.Join(dir, "missing"), "task-1"); err != nil || found || last.Seq != 0 {
		t.Fatalf("missing log: last=%#v found=%v err=%v", last, found, err)
	}

	if last, found, err := readLastTaskEvent(dir, "task-1"); !errors.Is(err, ErrEventLogReadIncomplete) || found || last.Seq != 0 {
		t.Fatalf("read failure: last=%#v found=%v err=%v", last, found, err)
	}

	filePath := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if last, found, err := readLastTaskEvent(filepath.Join(filePath, "child"), "task-1"); err == nil || errors.Is(err, ErrEventLogReadIncomplete) || found || last.Seq != 0 {
		t.Fatalf("open failure: last=%#v found=%v err=%v", last, found, err)
	}
}
