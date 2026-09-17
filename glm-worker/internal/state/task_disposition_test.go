package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetWithDispositionRejectsUnfinishedGenericReset(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ResetWithDisposition(""); err == nil || !strings.Contains(err.Error(), "explicit disposition") {
		t.Fatalf("unfinished generic reset did not fail closed: %v", err)
	}
	if got := st.ReadOr("task.id", ""); got != taskID {
		t.Fatalf("task.id changed after rejected reset: %q", got)
	}
	if got := st.TaskStatus(); got != TaskStatusAwaitingParentCompletion {
		t.Fatalf("task status changed after rejected reset: %q", got)
	}
	if _, err := st.CurrentTaskDisposition(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected reset wrote disposition: %v", err)
	}
}

func TestResetWithDispositionPersistsProvenanceUntilNewTaskAdmission(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
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
	if got := st.TaskStatus(); got != TaskStatusNone {
		t.Fatalf("status after reset = %q", got)
	}
	if got := st.ReadOr("task.id", ""); got != "" {
		t.Fatalf("task.id survived reset: %q", got)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("new task admission could not verify disposition: %v", err)
	}

	record, err := st.CurrentTaskDisposition()
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != taskID || record.FromStatus != string(TaskStatusAwaitingParentCompletion) || record.Disposition != TaskDispositionAbandon {
		t.Fatalf("unexpected disposition record: %#v", record)
	}
	lifecycle, err := ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range lifecycle {
		if item.Disposition == TaskDispositionAbandon {
			found = item.From == string(TaskStatusAwaitingParentCompletion) && item.To == string(TaskStatusNone) && item.Timestamp.Equal(record.RecordedAt)
		}
	}
	if !found {
		t.Fatalf("lifecycle does not preserve abandon provenance: %#v", lifecycle)
	}
	evidence, err := st.ArchivedTaskStatsEvidence(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Proven || evidence.Status != TaskStatusAwaitingParentCompletion {
		t.Fatalf("archived stats hid unfinished status: %#v", evidence)
	}

	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CurrentTaskDisposition(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new task transition did not consume disposition marker: %v", err)
	}
}

func TestResetWithDispositionAutoClassifiesRecoverableStop(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusInterrupted); err != nil {
		t.Fatal(err)
	}

	disposition, err := st.ResetWithDisposition("")
	if err != nil {
		t.Fatal(err)
	}
	if disposition != TaskDispositionRecovery {
		t.Fatalf("recoverable reset disposition = %q", disposition)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("recovery disposition was not admissible: %v", err)
	}
}

func TestResetWithDispositionRetryReusesDurableProvenance(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	originalRemove := removeStatePath
	failed := false
	removeStatePath = func(path string) error {
		if filepath.Base(path) == "worker.id" && !failed {
			failed = true
			return errors.New("injected reset failure")
		}
		return os.Remove(path)
	}
	if _, err := st.ResetWithDisposition(string(TaskDispositionAbandon)); err == nil {
		removeStatePath = originalRemove
		t.Fatal("partial reset unexpectedly succeeded")
	}
	removeStatePath = originalRemove
	defer func() { removeStatePath = originalRemove }()

	if got := st.ReadOr("task.id", ""); got != "" {
		t.Fatalf("test did not reach partial reset boundary: task.id=%q", got)
	}
	if _, err := st.ResetWithDisposition(""); err != nil {
		t.Fatalf("retry did not reuse durable disposition: %v", err)
	}
	if err := st.ValidateResetDispositionForNewTask(); err != nil {
		t.Fatalf("retry left unverifiable disposition: %v", err)
	}

	lifecycle, err := ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	dispositions := 0
	for _, item := range lifecycle {
		if item.Disposition != "" {
			dispositions++
		}
	}
	if dispositions != 1 {
		t.Fatalf("retry duplicated disposition lifecycle records: %d", dispositions)
	}
}
