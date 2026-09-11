package app

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecordQualityGateValidationPreservesRunAndSnapshotIdentity(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	record := qualityGateRunRecord{
		ValidationRunID: "0123456789abcdef0123456789abcdef",
		Form:            "go-test",
		Head:            "head",
		IndexDigest:     "index",
		WorktreeDigest:  "worktree",
		Status:          state.ValidationResultPass,
		ExitSource:      state.ValidationExitSourceTarget,
		DurationMS:      123,
		Log:             st.Path("quality-gate-runs/0123456789abcdef0123456789abcdef/gate.log"),
	}
	recordQualityGateValidation(st, record)

	data, err := os.ReadFile(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("validation event lines = %d", len(lines))
	}
	event, err := state.ParseTaskEventLine([]byte(lines[0]))
	if err != nil {
		t.Fatal(err)
	}
	validation := event.Validation
	if validation == nil {
		t.Fatal("validation event missing")
	}
	if validation.ValidationRunID != record.ValidationRunID || validation.GateClass != state.ValidationGateClassTest || validation.Suite != "go-test" {
		t.Fatalf("validation identity = %#v", validation)
	}
	wantSnapshot := state.ValidationSnapshotID(record.Head, record.IndexDigest, record.WorktreeDigest)
	if validation.SnapshotID != wantSnapshot || validation.Phase != "quality-gate" || validation.Attempt != state.ValidationAttemptInitial {
		t.Fatalf("validation binding = %#v", validation)
	}
	if validation.Result != state.ValidationResultPass || validation.DurationMS != 123 {
		t.Fatalf("validation result = %#v", validation)
	}
	if strings.Contains(validation.Evidence, st.Path("")) {
		t.Fatalf("validation evidence exposed absolute state path: %q", validation.Evidence)
	}
}
