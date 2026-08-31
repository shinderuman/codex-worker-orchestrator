package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestSetTaskStatusRecordsLifecycleTransitionsOnChangeOnly(t *testing.T) {
	st := newLifecycleTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	records, err := ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	if records[0].From != string(TaskStatusNone) || records[0].To != string(TaskStatusActive) {
		t.Fatalf("first transition = %#v", records[0])
	}
	if records[1].From != string(TaskStatusActive) || records[1].To != string(TaskStatusWaitingDecision) {
		t.Fatalf("second transition = %#v", records[1])
	}
	if records[0].Timestamp.IsZero() || records[1].Timestamp.Before(records[0].Timestamp) {
		t.Fatalf("timestamps are not monotonic: %#v", records)
	}
	if records[0].TaskID != taskID || records[1].TaskID != taskID {
		t.Fatalf("task attribution = %#v", records)
	}
}

func TestSetTaskStatusSkipsLifecycleWithoutTaskIdentity(t *testing.T) {
	st := newLifecycleTestStore(t)
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Path("lifecycle")); !os.IsNotExist(err) {
		t.Fatalf("lifecycle log should not exist without a task: %v", err)
	}
}

func newLifecycleTestStore(t *testing.T) *StateStore {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("b", 64),
		StateBase: filepath.Join(root, "state"),
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
