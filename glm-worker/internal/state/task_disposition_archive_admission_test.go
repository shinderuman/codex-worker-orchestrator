package state

import (
	"encoding/json"
	"os"
	"testing"
)

func newResetDispositionArchiveAdmissionFixture(t *testing.T) (*StateStore, string) {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetWithDisposition(string(TaskDispositionAbandon)); err != nil {
		t.Fatal(err)
	}
	return st, taskID
}

func TestValidateResetDispositionForNewTaskIgnoresMissingTaskStatsArchive(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	if err := os.Remove(st.TaskStatsArchivePath(taskID)); err != nil {
		t.Fatal(err)
	}

	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("canonical reset provenance depended on missing TaskStats archive: %v", err)
	}
}

func TestValidateResetDispositionForNewTaskIgnoresCorruptTaskStatsArchive(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	if err := os.WriteFile(st.TaskStatsArchivePath(taskID), []byte("{not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("canonical reset provenance depended on corrupt TaskStats archive: %v", err)
	}
}

func TestValidateResetDispositionForNewTaskIgnoresTaskStatsStatusMismatch(t *testing.T) {
	st, taskID := newResetDispositionArchiveAdmissionFixture(t)
	path := st.TaskStatsArchivePath(taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stats TaskStats
	if err := json.Unmarshal(data, &stats); err != nil {
		t.Fatal(err)
	}
	stats.Status = TaskStatusInterrupted
	data, err = json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("canonical reset provenance depended on TaskStats status: %v", err)
	}
}
