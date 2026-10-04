package workflow

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newRetentionGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため保持照合testをskipします: %v", err)
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "retention@example.invalid")
	run("config", "user.name", "retention test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.md")
	run("add", "--", repositoryharness.MarkerPath)
	run("commit", "-q", "-m", "initial")
	return repo
}

func runRetentionGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v失敗: %v: %s", args, err, output)
	}
}

func newGitStateStoreT(t *testing.T, repo string) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "retentionhash",
		RepoRoot:  repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	pinRepositoryHarnessActiveT(t, st)
	if err := state.CaptureGitBaseline(config.AppConfig{RepoRoot: repo}, st); err != nil {
		t.Fatal(err)
	}
	return st
}

func newGitWorkflowT(t *testing.T, st *state.StateStore, r *scriptedRunner, repo string) *Workflow {
	t.Helper()
	w := NewWorkflow(config.AppConfig{
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		TelemetryContent:      true,
		RepoRoot:              repo,
	}, st, r, io.Discard)
	w.admitCanonicalMutation = func() error { return nil }
	w.admitModelMutation = func(state.ResumeCheckpoint) (controllerModelCallGuard, error) { return controllerModelCallGuard{}, nil }
	w.temp = t.TempDir()
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	w.captureQualitySurface = func(string) (string, error) { return "", nil }
	clock := newFakeClock()
	w.now = clock.nowFunc
	w.sleep = clock.sleepFunc
	return w
}

func stopWorkflowInCall(t *testing.T, w *Workflow, st *state.StateStore, checkpoint state.ResumeCheckpoint) {
	t.Helper()
	r := w.runner.(*scriptedRunner)
	stop := attachStop(t, w)
	r.onRun = func() { stop.Request() }
	if err := st.Write("worker.id", "sess-retention"); err != nil {
		t.Fatal(err)
	}
	_, err := w.runModel(checkpoint)
	var stopped *runner.InterruptedCallError
	if !errors.As(err, &stopped) {
		t.Fatalf("InterruptedCallErrorを期待: %v", err)
	}
}

func retentionCheckpoint(t *testing.T, st *state.StateStore) state.ResumeCheckpoint {
	t.Helper()
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func TestInterruptedStopCapturesRetention(t *testing.T) {
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.md"), []byte("作業中\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newGitStateStoreT(t, repo)
	r := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, r, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.Head == "" {
		t.Fatalf("停止時snapshotが固定されていません: %#v", checkpoint.StopGitSnapshot)
	}
	if checkpoint.StopDirtyFiles == nil {
		t.Fatal("停止時dirty保持基準が固定されていません")
	}
	found := false
	for _, file := range checkpoint.StopDirtyFiles {
		if file.Path == "uncommitted.md" {
			found = true
			if file.IndexSHA != "" || file.WorktreeSHA == "" {
				t.Fatalf("untracked保持識別子が不正です: %#v", file)
			}
		}
	}
	if !found {
		t.Fatalf("untracked fileが保持基準に含まれません: %#v", checkpoint.StopDirtyFiles)
	}
	if !st.Exists("stop-worktree.patch") || !st.Exists("stop-index.patch") {
		t.Fatal("停止時recovery patchが保存されていません")
	}
}

func TestResumeInterruptedUntouchedPasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	writeRetentionFile(t, filepath.Join(repo, "uncommitted.md"), []byte("作業中\n"), 0o644)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("無変更resumeが保持照合を通過しません: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("task status = %s want complete", st.TaskStatus())
	}
}

func TestResumeInterruptedParentMetadataDeltaPasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	writeRetentionFile(t, filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v1\n"), 0o644)
	makeRetentionDir(t, filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	writeRetentionFile(t, filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644)
	writeRetentionFile(t, filepath.Join(repo, "IMPLEMENTATION_TASKS", "other-task.md"), []byte("task\n"), 0o644)

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("親管理metadata更新後のresumeが保持照合を通過しません: %v", err)
	}
}

func TestResumeInterruptedDirtyDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	writeRetentionFile(t, filepath.Join(repo, "uncommitted.md"), []byte("作業中\n"), 0o644)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())
	before := retentionCheckpoint(t, st)

	writeRetentionFile(t, filepath.Join(repo, "uncommitted.md"), []byte("衝突解決済み\n"), 0o644)
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("保持違反のresumeがWorkerErrorになりません: %v", err)
	}
	if after := retentionCheckpoint(t, st); after.StopKind != state.ResumeStopInterrupted || after.StopDirtyFiles == nil {
		t.Fatalf("fail closedが保持checkpointを破壊しています: %#v", after)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("保持違反でmodel呼出を実行しています: %v", resumeRunner.prompts)
	}

	writeRetentionFile(t, filepath.Join(repo, "uncommitted.md"), []byte("作業中\n"), 0o644)
	if before.StopDirtyFiles == nil {
		t.Fatal("停止時基準がありません")
	}
	retryRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	retryW := newGitWorkflowT(t, st, retryRunner, repo)
	if err := retryW.ExecuteResume(); err != nil {
		t.Fatalf("停止時内容へ復元後のresumeが通過しません: %v", err)
	}
}

func TestResumeInterruptedExecBitDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("作業中\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	if err := os.Chmod(filepath.Join(repo, "tracked.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("executable bit変化のresumeがWorkerErrorになりません: %v", err)
	}
	if !strings.Contains(workerErr.Message, "tracked.md(内容変化)") {
		t.Fatalf("fail closed理由がexecutable bit変化を指していません: %s", workerErr.Message)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
}

func TestResumeInterruptedForeignDirtyFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	writeRetentionFile(t, filepath.Join(repo, "foreign.txt"), []byte("外部書込み\n"), 0o644)
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("新規dirty検出がWorkerErrorになりません: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
}

func TestResumeInterruptedParentOnlyHeadAdvancePasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	writeRetentionFile(t, filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v1\n"), 0o644)
	runRetentionGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, repo, "commit", "-q", "-m", "plan")
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	writeRetentionFile(t, filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644)
	runRetentionGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, repo, "commit", "-q", "-m", "plan update")

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("親管理metadata commit後のresumeが保持照合を通過しません: %v", err)
	}
}

func TestResumeInterruptedHeadAdvanceOutsideParentPathsFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	writeRetentionFile(t, filepath.Join(repo, "tracked.md"), []byte("changed\n"), 0o644)
	runRetentionGit(t, repo, "add", "tracked.md")
	runRetentionGit(t, repo, "commit", "-q", "-m", "foreign integration")

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("親管理外HEAD変更のresumeがWorkerErrorになりません: %v", err)
	}
	if !strings.Contains(workerErr.Message, "親管理外file") {
		t.Fatalf("fail closed理由が親管理外HEAD変更を指していません: %s", workerErr.Message)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
}

func TestResumeInterruptedNonAncestorHeadFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	runRetentionGit(t, repo, "commit", "-q", "--amend", "-m", "amended")

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("非祖先HEAD移動のresumeがWorkerErrorになりません: %v", err)
	}
}

func TestResumeInterruptedLegacyCheckpointFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedInterruptedCheckpoint(t, st, "sess-legacy", "")

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("旧形式checkpointのresumeがWorkerErrorになりません: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("旧形式checkpointでmodel呼出を実行しています: %v", resumeRunner.prompts)
	}
}

func writeRetentionFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func makeRetentionDir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
}
