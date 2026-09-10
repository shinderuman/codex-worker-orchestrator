package parentactioncmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCurrentGuardRepairRecordIgnoredAfterTaskLeavesGuardRecovery(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	record.Status = state.GuardRepairComplete
	record.RepairedDigest = "digest-after"
	record.OriginalResumeObserved = true
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if _, ok := currentGuardRepairRecord(state.AttachStateStore(cfg)); ok {
		t.Fatal("completed guard repair intercepted a later non-guard resume state")
	}
}

func TestResumeWithRepairedWorkerRequiresGuardRecoveryStateExit(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, "package main\nfunc main() {}\n")

	err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block"))
	if err == nil {
		t.Fatal("zero-exit rebuilt worker without state transition was accepted")
	}
	got, loadErr := st.LoadGuardRepairRecord()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Status != state.GuardRepairFailed || got.OriginalResumeObserved {
		t.Fatalf("repair completion was recorded without original resume: %#v", got)
	}
}

func TestResumeWithRepairedWorkerRecordsOriginalResumeAfterStateExit(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	statusPath := strconv.Quote(st.Path("task.status"))
	source := fmt.Sprintf("package main\nimport \"os\"\nfunc main() { if err := os.WriteFile(%s, []byte(\"active\\n\"), 0600); err != nil { os.Exit(2) } }\n", statusPath)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, source)

	if err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block")); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("original task status = %s want active", st.TaskStatus())
	}
	if got.Status != state.GuardRepairComplete || !got.OriginalResumeObserved {
		t.Fatalf("original resume evidence was not recorded: %#v", got)
	}
}

func newGuardRepairLifecycleState(t *testing.T) (config.AppConfig, *state.StateStore, state.GuardRepairRecord) {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  root,
		StateBase: t.TempDir(),
		RepoHash:  "guard-repair-lifecycle",
		RepoShort: "guardrepair",
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		t.Fatal(err)
	}
	record := state.GuardRepairRecord{
		TaskID:         taskID,
		Phase:          "worker-new",
		Fingerprint:    "fingerprint",
		Strategy:       "bounded-guard-source-repair-v1",
		Status:         state.GuardRepairReady,
		Failure:        "guard recovery cannot capture current refs",
		RelevantDigest: "digest-before",
		RepairedDigest: "digest-after",
	}
	return cfg, st, record
}

func writeGuardRepairWorkerModule(t *testing.T, repoRoot, source string) {
	t.Helper()
	moduleRoot := filepath.Join(repoRoot, "glm-worker")
	mainDir := filepath.Join(moduleRoot, "cmd", "glm-worker")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module example.com/guardrepairtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}
