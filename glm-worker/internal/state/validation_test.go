package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestRecordValidationUsesCurrentTaskEventWhenTaskExists(t *testing.T) {
	st := newValidationTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	st.RecordValidation("quality-gate", "go-test", "", "pass", 0, 123, "quality-gate-logs/go-test-pass.log")

	data, err := os.ReadFile(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	record, err := ParseTaskEventLine(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != taskID || record.Kind != "validation" || record.Validation == nil {
		t.Fatalf("record = %#v", record)
	}
	if record.Validation.Attribution != "task" || record.Validation.Source != "quality-gate" ||
		record.Validation.Form != "go-test" || record.Validation.DurationMS != 123 {
		t.Fatalf("validation = %#v", record.Validation)
	}
}

func TestRecordValidationPersistsExplicitStandaloneAttribution(t *testing.T) {
	st := newValidationTestStore(t)
	st.RecordValidation("install-smoke", "install-smoke", "worker", "pass", 0, 42, "")

	file, err := os.Open(st.Path(standaloneValidationFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("standalone validation record missing")
	}
	var record TaskEventRecord
	if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.TaskID != "" || record.Validation == nil || record.Validation.Attribution != "standalone" {
		t.Fatalf("record = %#v", record)
	}
	if record.Validation.Scope != "worker" {
		t.Fatalf("scope = %q", record.Validation.Scope)
	}
}

func newValidationTestStore(t *testing.T) *StateStore {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("a", 64),
		StateBase: filepath.Join(root, "state"),
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
