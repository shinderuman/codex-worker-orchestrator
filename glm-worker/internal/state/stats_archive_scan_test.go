package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestAllTaskStatsWithArchiveScanReportsUnsupportedSkips(t *testing.T) {
	st := newTaskStatsArchiveScanTestStore(t)
	writeTaskStatsArchiveScanFixture(t, st, "accepted", TaskStats{
		Version:    taskStatsVersion,
		TaskID:     "accepted",
		StartedAt:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Status:     TaskStatusActive,
		ModelCalls: 7,
	})

	unsupportedRevision := marshalTaskStatsArchiveScanMap(t, map[string]any{
		"version":         taskStatsVersion,
		"schema_revision": taskStatsSchemaRevision + 1,
		"task_id":         "unsupported-revision",
		"model_calls":     99,
	})
	unsupportedMissingRevision := marshalTaskStatsArchiveScanMap(t, map[string]any{
		"version":     taskStatsVersion,
		"task_id":     "unsupported-missing-revision",
		"model_calls": 101,
	})
	writeRawTaskStatsArchiveScanFixture(t, st, "unsupported-revision", unsupportedRevision)
	writeRawTaskStatsArchiveScanFixture(t, st, "unsupported-missing-revision", unsupportedMissingRevision)

	result, err := st.AllTaskStatsWithArchiveScan()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.ArchiveScan.FilesConsidered, 3; got != want {
		t.Fatalf("files considered = %d, want %d", got, want)
	}
	if got, want := result.ArchiveScan.FilesAccepted, 1; got != want {
		t.Fatalf("files accepted = %d, want %d", got, want)
	}
	if got, want := result.ArchiveScan.UnsupportedSchemaOrRevisionSkipped, 2; got != want {
		t.Fatalf("unsupported skipped = %d, want %d", got, want)
	}
	if len(result.Stats) != 1 || result.Stats[0].TaskID != "accepted" || result.Stats[0].ModelCalls != 7 {
		t.Fatalf("accepted stats changed: %#v", result.Stats)
	}

	if _, err := decodeTaskStats(unsupportedMissingRevision); !errors.Is(err, errUnsupportedTaskStatsVersion) {
		t.Fatalf("missing schema_revision must remain unsupported, got %v", err)
	}
}

func TestAllTaskStatsWithArchiveScanReportsZeroUnsupportedSkips(t *testing.T) {
	st := newTaskStatsArchiveScanTestStore(t)
	for index, taskID := range []string{"accepted-a", "accepted-b"} {
		writeTaskStatsArchiveScanFixture(t, st, taskID, TaskStats{
			Version:    taskStatsVersion,
			TaskID:     taskID,
			StartedAt:  time.Date(2026, 9, index+1, 0, 0, 0, 0, time.UTC),
			Status:     TaskStatusActive,
			ModelCalls: index + 1,
		})
	}

	result, err := st.AllTaskStatsWithArchiveScan()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.ArchiveScan.FilesConsidered, 2; got != want {
		t.Fatalf("files considered = %d, want %d", got, want)
	}
	if got, want := result.ArchiveScan.FilesAccepted, 2; got != want {
		t.Fatalf("files accepted = %d, want %d", got, want)
	}
	if got := result.ArchiveScan.UnsupportedSchemaOrRevisionSkipped; got != 0 {
		t.Fatalf("unsupported skipped = %d, want 0", got)
	}
	if len(result.Stats) != 2 {
		t.Fatalf("accepted stats = %d, want 2", len(result.Stats))
	}
}

func newTaskStatsArchiveScanTestStore(t *testing.T) *StateStore {
	t.Helper()
	st, err := NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "task-stats-archive-scan-test",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func writeTaskStatsArchiveScanFixture(t *testing.T, st *StateStore, taskID string, stats TaskStats) {
	t.Helper()
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	writeRawTaskStatsArchiveScanFixture(t, st, taskID, data)

	decoded, err := decodeTaskStats(data)
	if err != nil {
		t.Fatalf("current schema fixture rejected: %v", err)
	}
	if decoded.TaskID != stats.TaskID {
		t.Fatalf("decoded task id = %q, want %q", decoded.TaskID, stats.TaskID)
	}
}

func marshalTaskStatsArchiveScanMap(t *testing.T, value map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeRawTaskStatsArchiveScanFixture(t *testing.T, st *StateStore, taskID string, data []byte) {
	t.Helper()
	path := st.TaskStatsArchivePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
