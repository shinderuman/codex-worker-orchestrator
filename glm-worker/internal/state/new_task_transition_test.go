package state

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

const (
	oldAtomicTaskID = "11111111-1111-4111-8111-111111111111"
	newAtomicTaskID = "22222222-2222-4222-8222-222222222222"
)

func TestNewTaskCanonicalTransitionRollsBackMajorMutationFailures(t *testing.T) {
	tests := []struct {
		name       string
		failRemove string
		failWrite  string
	}{
		{name: "remove old task identity", failRemove: "task.id"},
		{name: "invalidate sessions", failRemove: "worker.id"},
		{name: "remove task context", failRemove: "pending-decision"},
		{name: "clear parent evidence ledger", failRemove: parentEvidenceLedgerPath},
		{name: "rotate parent evidence lease", failWrite: parentEvidenceLeasePath},
		{name: "initialize parent review", failWrite: parentReviewStateFile},
		{name: "write new task status", failWrite: "task.status"},
		{name: "commit new task identity", failWrite: "task.id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newAtomicTransitionFixture(t)
			before, err := st.captureNewTaskTransitionSnapshot()
			if err != nil {
				t.Fatal(err)
			}

			if tc.failRemove != "" {
				injectOneNewTaskRemoveFailure(t, st.Path(tc.failRemove))
			}
			if tc.failWrite != "" {
				injectOneNewTaskWriteFailure(t, st.Path(tc.failWrite))
			}

			if _, err := st.startNewTaskWithID(newAtomicTaskID, false); err == nil {
				t.Fatal("StartNewTask unexpectedly succeeded")
			}
			after, err := st.captureNewTaskTransitionSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("canonical state changed after failed transition\nbefore=%#v\nafter=%#v", before, after)
			}
			if taskID, err := st.TaskID(); err != nil || taskID != oldAtomicTaskID {
				t.Fatalf("old task identity not restored: task=%q err=%v", taskID, err)
			}
			if status := st.TaskStatus(); status != TaskStatusWaitingDecision {
				t.Fatalf("old task status not restored: %q", status)
			}
		})
	}
}

func TestNewTaskObservationalStatsFailureDoesNotUndoCanonicalCommit(t *testing.T) {
	st := newAtomicTransitionFixture(t)
	oldLease, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}
	injectOneNewTaskWriteFailure(t, st.Path(currentStatsFile))

	taskID, err := st.startNewTaskWithID(newAtomicTaskID, false)
	if err != nil {
		t.Fatalf("observational stats failure rejected committed task: %v", err)
	}
	if taskID != newAtomicTaskID {
		t.Fatalf("task ID = %q", taskID)
	}
	if current, err := st.TaskID(); err != nil || current != newAtomicTaskID {
		t.Fatalf("committed task identity = %q err=%v", current, err)
	}
	if status := st.TaskStatus(); status != TaskStatusActive {
		t.Fatalf("committed task status = %q", status)
	}
	if _, err := st.loadParentReviewState(); err != nil {
		t.Fatalf("committed parent review state invalid: %v", err)
	}
	newLease, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if newLease != oldLease+1 {
		t.Fatalf("parent evidence lease = %d, want %d", newLease, oldLease+1)
	}
	for _, name := range []string{"worker.id", "worker.ready", "reviewer.id", "reviewer.ready", "pending-decision"} {
		if st.Exists(name) {
			t.Fatalf("old task state survived committed transition: %s", name)
		}
	}
}

func newAtomicTransitionFixture(t *testing.T) *StateStore {
	t.Helper()
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.startNewTaskWithID(oldAtomicTaskID, false); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"worker.id":        "old-worker",
		"worker.ready":     "1",
		"reviewer.id":      "old-reviewer",
		"reviewer.ready":   "1",
		"pending-decision": "old-decision",
		"baseline-head":    "old-head",
	} {
		if err := st.Write(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetTaskStatus(TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveParentEvidenceLedgerEntry(ParentEvidenceLedgerEntry{
		Surface:     ParentEvidenceSurfaceStatus,
		Digest:      "old-digest",
		Origin:      ParentEvidenceOriginEvidence,
		OwnerCallID: "old-call",
	}); err != nil {
		t.Fatal(err)
	}
	return st
}

func injectOneNewTaskRemoveFailure(t *testing.T, target string) {
	t.Helper()
	original := removeStatePath
	failed := false
	removeStatePath = func(path string) error {
		if !failed && path == target {
			failed = true
			return errors.New("injected new-task remove failure")
		}
		return original(path)
	}
	t.Cleanup(func() { removeStatePath = original })
}

func injectOneNewTaskWriteFailure(t *testing.T, target string) {
	t.Helper()
	original := writeFileAtomic
	failed := false
	writeFileAtomic = func(path string, data []byte, mode os.FileMode) error {
		if !failed && path == target {
			failed = true
			return errors.New("injected new-task write failure")
		}
		return original(path, data, mode)
	}
	t.Cleanup(func() { writeFileAtomic = original })
}
