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

func eventLogLine(t *testing.T, seq int) string {
	t.Helper()
	data, err := json.Marshal(state.TaskEventRecord{
		Version: 1, TaskID: "task-1", CallID: "call-1",
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
	first := eventLogLine(t, 1)
	third := eventLogLine(t, 3)
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
		{name: "overlong middle then valid tail", content: first + tooLong + third, partial: true},
		{name: "overlong tail", content: first + tooLong, partial: true},
		{name: "empty", content: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			last, found, err := ReadLastTaskEvent(path)
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
	if last, found, err := ReadLastTaskEvent(filepath.Join(dir, "missing")); err != nil || found || last.Seq != 0 {
		t.Fatalf("missing log: last=%#v found=%v err=%v", last, found, err)
	}

	// Opening a directory works on Unix, but scanning its contents fails.
	if last, found, err := ReadLastTaskEvent(dir); !errors.Is(err, ErrEventLogReadIncomplete) || found || last.Seq != 0 {
		t.Fatalf("read failure: last=%#v found=%v err=%v", last, found, err)
	}

	filePath := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if last, found, err := ReadLastTaskEvent(filepath.Join(filePath, "child")); err == nil || errors.Is(err, ErrEventLogReadIncomplete) || found || last.Seq != 0 {
		t.Fatalf("open failure: last=%#v found=%v err=%v", last, found, err)
	}
}
