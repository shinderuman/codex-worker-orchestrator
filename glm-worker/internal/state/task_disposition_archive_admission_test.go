package state

import (
	"encoding/json"
	"io"
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

func TestResetDispositionIgnoresLostTaskStatsStatusWrite(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	restoreWarnings := RedirectStatsWarnings(io.Discard)
	defer restoreWarnings()
	failWritesFor(t, st, currentStatsFile)

	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	disposition, err := st.ResetWithDisposition(string(TaskDispositionAbandon))
	if err != nil {
		t.Fatal(err)
	}
	if disposition != TaskDispositionAbandon {
		t.Fatalf("disposition = %q", disposition)
	}
	record, err := st.CurrentTaskDisposition()
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != taskID || record.FromStatus != string(TaskStatusAwaitingParentCompletion) {
		t.Fatalf("canonical reset record = %#v", record)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("lost TaskStats status write blocked new-task admission: %v", err)
	}
	evidence, err := st.ArchivedTaskStatsEvidence(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Proven || evidence.Status != TaskStatusActive {
		t.Fatalf("test did not preserve stale observational TaskStats status: %#v", evidence)
	}
}
