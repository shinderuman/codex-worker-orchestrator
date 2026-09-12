package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchivedTaskStatsEvidenceRequiresCurrentSchemaRevision(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	current, err := json.Marshal(TaskStats{Version: taskStatsVersion, TaskID: taskID, Status: TaskStatusComplete})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(st.TaskStatsArchivePath(taskID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskStatsArchivePath(taskID), current, 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := st.ArchivedTaskStatsEvidence(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Proven || evidence.TaskID != taskID || evidence.Status != TaskStatusComplete {
		t.Fatalf("current archive evidence = %#v", evidence)
	}

	var old map[string]any
	if err := json.Unmarshal(current, &old); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []any{float64(0), nil} {
		if revision == nil {
			delete(old, "schema_revision")
		} else {
			old["schema_revision"] = revision
		}
		data, err := json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(st.TaskStatsArchivePath(taskID), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ArchivedTaskStatsEvidence(taskID); !errors.Is(err, errUnsupportedTaskStatsVersion) {
			t.Fatalf("old schema revision was accepted: revision=%v err=%v", revision, err)
		}
	}
}
