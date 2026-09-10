package parentactioncmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
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

func TestExecuteResumeUsesReadyRepairWithoutRepeatingNormalResume(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	statusPath := strconv.Quote(st.Path("task.status"))
	source := fmt.Sprintf("package main\nimport \"os\"\nfunc main() { if err := os.WriteFile(%s, []byte(\"active\\n\"), 0600); err != nil { os.Exit(2) } }\n", statusPath)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, source)
	persistReadyGuardRepair(t, cfg, st, &record)
	marker := installFailingNormalWorker(t)

	if err := executeResumeWithGuardRepair(cfg, io.Discard, io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("persisted ready repair redispatched the broken normal resume")
	}
	got, err := st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.GuardRepairComplete || !got.OriginalResumeObserved {
		t.Fatalf("ready repair did not continue to original resume: %#v", got)
	}
}

func TestExecuteResumeDoesNotRepeatFailedRepairEvidence(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	digest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	record.Status = state.GuardRepairFailed
	record.RelevantDigest = digest
	record.RepairedDigest = ""
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	marker := installFailingNormalWorker(t)

	err = executeResumeWithGuardRepair(cfg, io.Discard, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "already failed") {
		t.Fatalf("failed repair evidence did not converge without redispatch: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed repair evidence redispatched the broken normal resume")
	}
}

func TestResumeWithRepairedWorkerRequiresGuardRecoveryStateExit(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, "package main\nfunc main() {}\n")
	persistReadyGuardRepair(t, cfg, st, &record)

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
	persistReadyGuardRepair(t, cfg, st, &record)

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
	runFinalizationGit(t, root, "init", "-q")
	runFinalizationGit(t, root, "config", "user.email", "guard-repair@example.invalid")
	runFinalizationGit(t, root, "config", "user.name", "guard repair test")
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
		Strategy:       guardrepair.StrategySourcePatch,
		Status:         state.GuardRepairReady,
		Failure:        "guard recovery cannot capture current refs",
		RelevantDigest: "digest-before",
		RepairedDigest: "digest-after",
	}
	return cfg, st, record
}

func persistReadyGuardRepair(t *testing.T, cfg config.AppConfig, st *state.StateStore, record *state.GuardRepairRecord) {
	t.Helper()
	digest, err := guardrepair.RelevantDigest(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	record.RepairedDigest = digest
	if err := st.SaveGuardRepairRecord(*record); err != nil {
		t.Fatal(err)
	}
	dirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:           state.ResumeStageWorker,
		Phase:           record.Phase,
		Role:            state.WorkerRole,
		Model:           "worker-model",
		Prompt:          "prompt",
		Request:         "request",
		StopKind:        state.ResumeStopGuardRecoverable,
		GuardFailure:    record.Failure,
		StopGitSnapshot: &state.GitSnapshot{Head: "stopped"},
		StopDirtyFiles:  dirty,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
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

func installFailingNormalWorker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "normal-worker-called")
	script := fmt.Sprintf("#!/bin/sh\n: > %q\nexit 91\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}
