package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCopyGuardRepairChangesRollsBackPartialFailure(t *testing.T) {
	repo := t.TempDir()
	worktree := t.TempDir()
	first := "glm-worker/internal/workflow/guard_recovery.go"
	second := "glm-worker/internal/workflow/guard_recovery_test.go"
	writeGuardRepairTestFile(t, repo, first, "original source\n")
	writeGuardRepairTestFile(t, repo, second, "original test\n")
	writeGuardRepairTestFile(t, worktree, first, "repaired source\n")

	if _, err := copyGuardRepairChangesWithRollback(worktree, repo, []string{first, second}); err == nil {
		t.Fatal("partial repair copy unexpectedly succeeded")
	}
	assertGuardRepairTestFile(t, repo, first, "original source\n")
	assertGuardRepairTestFile(t, repo, second, "original test\n")
}

func TestCopyGuardRepairChangesRejectsOriginalSymlink(t *testing.T) {
	repo := t.TempDir()
	worktree := t.TempDir()
	path := "glm-worker/internal/workflow/guard_recovery.go"
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, full); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeGuardRepairTestFile(t, worktree, path, "repaired\n")

	if _, err := copyGuardRepairChangesWithRollback(worktree, repo, []string{path}); err == nil {
		t.Fatal("symlink repair destination unexpectedly accepted")
	}
	assertGuardRepairTestFile(t, filepath.Dir(outside), filepath.Base(outside), "outside\n")
}

func TestIntegrateGuardRepairCandidateRollsBackAfterCheckpointLoss(t *testing.T) {
	repo := t.TempDir()
	first := "glm-worker/internal/workflow/guard_recovery.go"
	second := "glm-worker/internal/workflow/guard_recovery_test.go"
	writeGuardRepairTestFile(t, repo, first, "original source\n")
	writeGuardRepairTestFile(t, repo, second, "original test\n")
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "guard-repair@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "guard repair test")
	runFinalizationGit(t, repo, "add", ".")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")

	cfg := config.AppConfig{RepoRoot: repo, StateBase: t.TempDir(), RepoHash: "guard-repair-rollback"}
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
	digest, err := guardrepair.RelevantDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := state.CaptureRepositoryBoundarySnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := state.CaptureStopDirtyFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{
		Stage:           state.ResumeStageWorker,
		Phase:           "worker-new",
		Role:            state.WorkerRole,
		Model:           "worker-model",
		Prompt:          "prompt",
		Request:         "request",
		StopKind:        state.ResumeStopGuardRecoverable,
		GuardFailure:    "capture failed",
		StopGitSnapshot: &boundary,
		StopDirtyFiles:  dirty,
	}
	record := state.GuardRepairRecord{
		TaskID:         taskID,
		Phase:          checkpoint.Phase,
		Fingerprint:    "fingerprint",
		Strategy:       guardrepair.StrategySourcePatch,
		Status:         state.GuardRepairRunning,
		Failure:        checkpoint.GuardFailure,
		RelevantDigest: digest,
	}
	worktree := t.TempDir()
	writeGuardRepairTestFile(t, worktree, first, "repaired source\n")
	writeGuardRepairTestFile(t, worktree, second, "repaired test\n")
	candidate := guardRepairCandidate{worktree: worktree, changed: []string{first, second}}

	if _, err := integrateGuardRepairCandidate(cfg, st, record, guardRepairOrigin{checkpoint: checkpoint, boundary: boundary}, candidate); err == nil {
		t.Fatal("missing persisted checkpoint unexpectedly accepted")
	}
	assertGuardRepairTestFile(t, repo, first, "original source\n")
	assertGuardRepairTestFile(t, repo, second, "original test\n")
	restored, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if restored.StopKind != state.ResumeStopGuardRecoverable || restored.Phase != checkpoint.Phase {
		t.Fatalf("restored checkpoint = %#v", restored)
	}
}

func writeGuardRepairTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertGuardRepairTestFile(t *testing.T, root, path, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("file content = %q want %q", data, want)
	}
}
