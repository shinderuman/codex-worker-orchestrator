package executionunit

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecordDispositionBindsExecutionUnitToCurrentTask(t *testing.T) {
	cfg := config.AppConfig{StateBase: filepath.Join(t.TempDir(), "state"), RepoHash: "test-repo"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	const taskPath = "IMPLEMENTATION_TASKS/current.md"
	if err := st.Write("active-task", taskPath); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if err := RecordDisposition(st, taskPath, ExecutionUnitSingle, now); err != nil {
		t.Fatal(err)
	}

	got, err := CurrentDisposition(st)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.TaskID != taskID || got.ActiveTaskPath != taskPath || got.ExecutionUnit != ExecutionUnitSingle || !got.UpdatedAt.Equal(now) {
		t.Fatalf("disposition = %#v", got)
	}
}

func TestCurrentDispositionFailsClosedWhenActiveTaskChanges(t *testing.T) {
	cfg := config.AppConfig{StateBase: filepath.Join(t.TempDir(), "state"), RepoHash: "test-repo"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/one.md"); err != nil {
		t.Fatal(err)
	}
	if err := RecordDisposition(st, "IMPLEMENTATION_TASKS/one.md", ExecutionUnitMilestones, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/two.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := CurrentDisposition(st); err == nil {
		t.Fatal("stale execution-unit disposition was accepted")
	}
}

func TestNewTaskClearsExecutionUnitDisposition(t *testing.T) {
	cfg := config.AppConfig{StateBase: filepath.Join(t.TempDir(), "state"), RepoHash: "test-repo"}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/one.md"); err != nil {
		t.Fatal(err)
	}
	if err := RecordDisposition(st, "IMPLEMENTATION_TASKS/one.md", ExecutionUnitSingle, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDisposition(st)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("fresh task retained execution-unit disposition: %#v", got)
	}
}
