package parentactioncmd

import (
	"encoding/json"
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
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleEvidenceWorkerSource(t, st, state.TaskStatusActive, false))
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

func TestExecuteResumeDoesNotRepeatFailedRepairedEvidence(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, "package main\nfunc main() {}\n")
	persistReadyGuardRepair(t, cfg, st, &record)
	record.Status = state.GuardRepairFailed
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	marker := installFailingNormalWorker(t)

	err := executeResumeWithGuardRepair(cfg, io.Discard, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "already failed") {
		t.Fatalf("failed repaired evidence did not converge without redispatch: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed repaired evidence redispatched the broken normal resume")
	}
}

func TestResumeWithRepairedWorkerRequiresGuardRecoveryStateExit(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleEvidenceWorkerSource(t, st, "", false))
	persistReadyGuardRepair(t, cfg, st, &record)

	err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block"))
	if err == nil || !strings.Contains(err.Error(), "did not leave guard-recoverable state") {
		t.Fatalf("canonical resume evidence without state exit was accepted: %v", err)
	}
	got, loadErr := st.LoadGuardRepairRecord()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Status != state.GuardRepairFailed || got.OriginalResumeObserved {
		t.Fatalf("repair completion was recorded without guard recovery exit: %#v", got)
	}
}

func TestResumeWithRepairedWorkerRejectsResumeCounterWithoutLifecycleEvidence(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleWorkerSource(t, st, state.TaskStatusActive, true))
	persistReadyGuardRepair(t, cfg, st, &record)

	err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block"))
	requireGuardRepairLifecycleFailure(t, st, err, "TaskStats resume counter was accepted as original resume evidence")
}

func TestResumeWithRepairedWorkerRejectsStaleTransitionAttempt(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleEvidenceWorkerSource(t, st, state.TaskStatusActive, true))
	persistReadyGuardRepair(t, cfg, st, &record)

	err := resumeWithRepairedWorker(cfg, st, record, io.Discard, io.Discard, nil, errors.New("initial self-block"))
	requireGuardRepairLifecycleFailure(t, st, err, "stale transition attempt was accepted")
}

func requireGuardRepairLifecycleFailure(t *testing.T, st *state.StateStore, err error, message string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "did not enter original resume lifecycle") {
		t.Fatalf("%s: %v", message, err)
	}
	got, loadErr := st.LoadGuardRepairRecord()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got.Status != state.GuardRepairFailed || got.OriginalResumeObserved {
		t.Fatalf("failed resume recorded repair completion: %#v", got)
	}
}

func TestResumeWithRepairedWorkerRecordsOriginalResumeAfterLifecycleEntry(t *testing.T) {
	cfg, st, record := newGuardRepairLifecycleState(t)
	writeGuardRepairWorkerModule(t, cfg.RepoRoot, guardRepairLifecycleEvidenceWorkerSource(t, st, state.TaskStatusActive, false))
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
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ResumeCommands != 0 {
		t.Fatalf("fixture unexpectedly relied on TaskStats resume counter: %d", stats.ResumeCommands)
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

func guardRepairLifecycleWorkerSource(t *testing.T, st *state.StateStore, status state.TaskStatus, recordResume bool) string {
	t.Helper()
	body := fmt.Sprintf("if err := os.WriteFile(%s, []byte(%q), 0600); err != nil { os.Exit(2) }", strconv.Quote(st.Path("task.status")), string(status)+"\n")
	if recordResume {
		data, err := os.ReadFile(st.Path("task-stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		var stats state.TaskStats
		if err := json.Unmarshal(data, &stats); err != nil {
			t.Fatal(err)
		}
		stats.ResumeCommands++
		data, err = json.MarshalIndent(stats, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		body += fmt.Sprintf("; if err := os.WriteFile(%s, []byte(%q), 0600); err != nil { os.Exit(3) }", strconv.Quote(st.Path("task-stats.json")), string(data))
	}
	return "package main\nimport \"os\"\nfunc main() { " + body + " }\n"
}

func guardRepairLifecycleEvidenceWorkerSource(t *testing.T, st *state.StateStore, status state.TaskStatus, staleAttempt bool) string {
	t.Helper()
	body := ""
	if status != "" {
		body = fmt.Sprintf("if err := os.WriteFile(%s, []byte(%q), 0600); err != nil { os.Exit(2) }; ", strconv.Quote(st.Path("task.status")), string(status)+"\n")
	}
	attempt := fmt.Sprintf("os.Getenv(%q)", state.GuardRepairResumeAttemptEnv)
	if staleAttempt {
		attempt = strconv.Quote("55555555-5555-4555-8555-555555555555")
	}
	body += fmt.Sprintf(`
	resumeData, err := os.ReadFile(%s)
	if err != nil { os.Exit(3) }
	var checkpoint map[string]any
	if err := json.Unmarshal(resumeData, &checkpoint); err != nil { os.Exit(4) }
	canonical, err := json.Marshal(checkpoint)
	if err != nil { os.Exit(5) }
	sum := sha256.Sum256(canonical)
	taskData, err := os.ReadFile(%s)
	if err != nil { os.Exit(6) }
	evidence := map[string]any{
		"version": 1,
		"task_id": strings.TrimSpace(string(taskData)),
		"attempt_id": %s,
		"checkpoint_digest": hex.EncodeToString(sum[:]),
		"stop_kind": checkpoint["stop_kind"],
		"phase": checkpoint["phase"],
		"observed_at": time.Now().UTC(),
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil { os.Exit(7) }
	data = append(data, '\n')
	if err := os.WriteFile(%s, data, 0600); err != nil { os.Exit(8) }
`, strconv.Quote(st.Path("resume-state.json")), strconv.Quote(st.Path("task.id")), attempt, strconv.Quote(st.Path("resume-transition.json")))
	return "package main\nimport (\"crypto/sha256\"; \"encoding/hex\"; \"encoding/json\"; \"os\"; \"strings\"; \"time\")\nfunc main() { " + body + " }\n"
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
