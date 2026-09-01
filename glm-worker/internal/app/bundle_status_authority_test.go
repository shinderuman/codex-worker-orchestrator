package app

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBundleCurrentTaskStatusDoesNotFallBackToStats(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Status != state.TaskStatusWaitingSolReview {
		t.Fatalf("stats status = %q", stats.Status)
	}
	if err := st.Remove("task.status"); err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.TaskID != taskID || output.TaskStatus != string(state.TaskStatusNone) {
		t.Fatalf("output = %#v", output)
	}
	if !slices.Contains(output.CoverageReasons, "task-status:"+string(state.TaskStatusNone)) {
		t.Fatalf("coverage reasons = %v", output.CoverageReasons)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.CurrentTask || manifest.TaskStatus != string(state.TaskStatusNone) {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestBundleHistoricalTaskKeepsArchivedStatsStatus(t *testing.T) {
	_, st := newBundleTestState(t)
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	task, err := selectBundleTask(st, oldTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Current {
		t.Fatalf("historical task marked current: %#v", task)
	}
	if task.Status != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("historical status = %q", task.Status)
	}
}
