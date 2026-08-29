package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExternalFeasibilitySnapshotsCurrentTaskAuthority(t *testing.T) {
	st := newStateStoreT(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	w := newWorkflowT(t, st, &scriptedRunner{})
	rel := filepath.ToSlash(filepath.Join("IMPLEMENTATION_TASKS", "014-test.md"))
	full := filepath.Join(w.config.RepoRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Task\n\n## External feasibility\nstatus: not-applicable\n")
	if err := os.WriteFile(full, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, rel); err != nil {
		t.Fatal(err)
	}
	if _, err := w.gateExternalFeasibility("worker-new", false); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(st.TaskAuthorityContentPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("authority snapshot = %q, want %q", got, content)
	}
}
