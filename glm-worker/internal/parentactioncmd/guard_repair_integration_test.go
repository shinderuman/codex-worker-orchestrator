package parentactioncmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/guardrepair"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type guardRepairIntegrationFixture struct {
	cfg      config.AppConfig
	st       *state.StateStore
	record   state.GuardRepairRecord
	origin   guardRepairOrigin
	worktree string
	first    string
	second   string
}

func TestGuardRepairIntegrationRecoversAfterPartialCopyInterruption(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	if _, err := beginGuardRepairIntegration(fixture.st, fixture.record, fixture.origin, fixture.cfg.RepoRoot, fixture.worktree, []string{fixture.first, fixture.second}); err != nil {
		t.Fatal(err)
	}
	if err := copyGuardRepairPath(fixture.worktree, fixture.cfg.RepoRoot, fixture.first); err != nil {
		t.Fatal(err)
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "repaired source\n")
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.second, "original test\n")

	if err := recoverGuardRepairIntegrationIfNeeded(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "original source\n")
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.second, "original test\n")
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); !errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		t.Fatalf("integration journal remains after rollback: %v", err)
	}
	record, err := fixture.st.LoadGuardRepairRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != state.GuardRepairRequested || record.RepairedDigest != "" {
		t.Fatalf("recovered guard repair record = %#v", record)
	}
	checkpoint, err := fixture.st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.StopKind != state.ResumeStopGuardRecoverable || checkpoint.Phase != fixture.origin.checkpoint.Phase {
		t.Fatalf("recovered resume checkpoint = %#v", checkpoint)
	}
	if diff := state.DescribeStopDirtyDiff(fixture.origin.checkpoint.StopDirtyFiles, checkpoint.StopDirtyFiles); diff != "" {
		t.Fatalf("recovered resume checkpoint dirty state differs: %s", diff)
	}
}

func TestGuardRepairIntegrationRejectsLaterEditOnJournaledPath(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	if _, err := beginGuardRepairIntegration(fixture.st, fixture.record, fixture.origin, fixture.cfg.RepoRoot, fixture.worktree, []string{fixture.first, fixture.second}); err != nil {
		t.Fatal(err)
	}
	if err := copyGuardRepairPath(fixture.worktree, fixture.cfg.RepoRoot, fixture.first); err != nil {
		t.Fatal(err)
	}
	writeGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "later user edit\n")

	err := recoverGuardRepairIntegrationIfNeeded(fixture.cfg, fixture.st)
	if err == nil || !strings.Contains(err.Error(), "changed after integration stopped") {
		t.Fatalf("later edit recovery = %v", err)
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "later user edit\n")
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); err != nil {
		t.Fatalf("rejected recovery discarded journal: %v", err)
	}
}

func TestGuardRepairIntegrationRejectsStaleTaskJournalWithoutApplyingIt(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	if _, err := beginGuardRepairIntegration(fixture.st, fixture.record, fixture.origin, fixture.cfg.RepoRoot, fixture.worktree, []string{fixture.first, fixture.second}); err != nil {
		t.Fatal(err)
	}
	if err := copyGuardRepairPath(fixture.worktree, fixture.cfg.RepoRoot, fixture.first); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.Write("task.id", "foreign-task"); err != nil {
		t.Fatal(err)
	}

	if err := recoverGuardRepairIntegrationIfNeeded(fixture.cfg, fixture.st); err == nil {
		t.Fatal("foreign-task integration journal unexpectedly applied")
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "repaired source\n")
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); err != nil {
		t.Fatalf("stale journal was discarded: %v", err)
	}
}

func TestGuardRepairIntegrationRejectsProvenanceMismatchWithoutApplyingIt(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	if _, err := beginGuardRepairIntegration(fixture.st, fixture.record, fixture.origin, fixture.cfg.RepoRoot, fixture.worktree, []string{fixture.first, fixture.second}); err != nil {
		t.Fatal(err)
	}
	if err := copyGuardRepairPath(fixture.worktree, fixture.cfg.RepoRoot, fixture.first); err != nil {
		t.Fatal(err)
	}
	mismatch := fixture.record
	mismatch.Fingerprint = "different-fingerprint"
	if err := fixture.st.SaveGuardRepairRecord(mismatch); err != nil {
		t.Fatal(err)
	}

	if err := recoverGuardRepairIntegrationIfNeeded(fixture.cfg, fixture.st); err == nil {
		t.Fatal("provenance-mismatched integration journal unexpectedly applied")
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "repaired source\n")
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); err != nil {
		t.Fatalf("mismatched journal was discarded: %v", err)
	}
}

func TestGuardRepairIntegrationRollbackFailureRetainsJournal(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	if _, err := beginGuardRepairIntegration(fixture.st, fixture.record, fixture.origin, fixture.cfg.RepoRoot, fixture.worktree, []string{fixture.first, fixture.second}); err != nil {
		t.Fatal(err)
	}
	if err := copyGuardRepairPath(fixture.worktree, fixture.cfg.RepoRoot, fixture.first); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(fixture.cfg.RepoRoot, filepath.FromSlash(fixture.first))
	if err := os.Remove(firstPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(firstPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := recoverGuardRepairIntegrationIfNeeded(fixture.cfg, fixture.st); err == nil {
		t.Fatal("rollback failure unexpectedly succeeded")
	}
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); err != nil {
		t.Fatalf("rollback failure discarded journal: %v", err)
	}
	info, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("rollback failure fixture no longer blocks file restoration")
	}
}

func TestGuardRepairIntegrationSuccessfulReadyPersistRemovesJournal(t *testing.T) {
	fixture := newGuardRepairIntegrationFixture(t)
	candidate := guardRepairCandidate{
		worktree: fixture.worktree,
		changed:  []string{fixture.first, fixture.second},
	}
	rollback, err := integrateGuardRepairCandidate(fixture.cfg, fixture.st, fixture.record, fixture.origin, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if rollback == nil {
		t.Fatal("successful integration did not return rollback handle")
	}
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); err != nil {
		t.Fatalf("integration journal missing before ready persist: %v", err)
	}
	repairedDigest, err := guardrepair.RelevantDigest(fixture.cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	ready := fixture.record
	ready.Status = state.GuardRepairReady
	ready.RepairedDigest = repairedDigest
	if err := persistReadyGuardRepairIntegration(fixture.st, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.LoadGuardRepairIntegrationJournal(); !errors.Is(err, state.ErrNoGuardRepairIntegrationJournal) {
		t.Fatalf("successful integration retained journal: %v", err)
	}
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.first, "repaired source\n")
	assertGuardRepairTestFile(t, fixture.cfg.RepoRoot, fixture.second, "repaired test\n")
}

func newGuardRepairIntegrationFixture(t *testing.T) guardRepairIntegrationFixture {
	t.Helper()
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

	cfg := config.AppConfig{
		RepoRoot:  repo,
		StateBase: t.TempDir(),
		RepoHash:  "guard-repair-integration",
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
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
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
	if err := st.SaveGuardRepairRecord(record); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	writeGuardRepairTestFile(t, worktree, first, "repaired source\n")
	writeGuardRepairTestFile(t, worktree, second, "repaired test\n")
	return guardRepairIntegrationFixture{
		cfg:      cfg,
		st:       st,
		record:   record,
		origin:   guardRepairOrigin{checkpoint: checkpoint, boundary: boundary},
		worktree: worktree,
		first:    first,
		second:   second,
	}
}
