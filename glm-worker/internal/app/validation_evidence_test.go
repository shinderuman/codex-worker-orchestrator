package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityGateRecordsTaskValidationEvidence(t *testing.T) {
	shimDir, _ := writeQualityGateGoShim(t, filepath.Join(t.TempDir(), "absent-flag"))
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runQualityGate("go-test", st, &stdout); err != nil {
		t.Fatal(err)
	}
	record := readSingleValidationEvent(t, st, taskID)
	if record.Validation.Source != "quality-gate" || record.Validation.Form != "go-test" ||
		record.Validation.Result != "pass" || record.Validation.Attribution != "task" {
		t.Fatalf("validation = %#v", record.Validation)
	}
	if record.Validation.Evidence != "quality-gate-logs/go-test-pass.log" {
		t.Fatalf("evidence = %q", record.Validation.Evidence)
	}
}

func TestInstallSmokeRecordsTaskValidationEvidence(t *testing.T) {
	cfg, st, _, _ := newInstallSmokeEnv(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runInstallSmoke("worker", cfg, st, &stdout); err != nil {
		t.Fatal(err)
	}
	record := readSingleValidationEvent(t, st, taskID)
	if record.Validation.Source != "install-smoke" || record.Validation.Form != "install-smoke" ||
		record.Validation.Scope != "worker" || record.Validation.Result != "pass" ||
		record.Validation.Attribution != "task" {
		t.Fatalf("validation = %#v", record.Validation)
	}
}

func readSingleValidationEvent(t *testing.T, st *state.StateStore, taskID string) state.TaskEventRecord {
	t.Helper()
	data, err := os.ReadFile(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	record, err := state.ParseTaskEventLine(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.Kind != "validation" || record.Validation == nil {
		t.Fatalf("record = %#v", record)
	}
	return record
}
