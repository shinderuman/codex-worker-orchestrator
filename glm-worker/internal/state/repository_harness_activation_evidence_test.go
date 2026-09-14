package state

import (
	"os"
	"testing"
)

func TestRepositoryHarnessActivationEvidenceRoundTrip(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRepositoryHarnessActivation(true); err != nil {
		t.Fatal(err)
	}

	active, known, err := st.ReadRepositoryHarnessActivation(taskID)
	if err != nil || !known || !active {
		t.Fatalf("activation evidence = active:%v known:%v err:%v", active, known, err)
	}
}

func TestRepositoryHarnessActivationEvidenceMissingIsUnknown(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	active, known, err := st.ReadRepositoryHarnessActivation(taskID)
	if err != nil || known || active {
		t.Fatalf("missing activation evidence = active:%v known:%v err:%v", active, known, err)
	}
}

func TestRepositoryHarnessActivationEvidenceRejectsMalformedRecord(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	path := st.repositoryHarnessActivationEvidencePath(taskID)
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, known, err := st.ReadRepositoryHarnessActivation(taskID); err == nil || known {
		t.Fatalf("malformed activation evidence accepted: known=%v err=%v", known, err)
	}
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if os.IsPathSeparator(path[i]) {
			return path[:i]
		}
	}
	return "."
}
