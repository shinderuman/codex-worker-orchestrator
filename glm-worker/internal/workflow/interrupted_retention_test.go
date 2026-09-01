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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type isolationGateFixture struct {
	repo       string
	st         *state.StateStore
	taskID     string
	stopHead   string
	checkpoint state.ResumeCheckpoint
}

const isolationWorktreePath = "/nonexistent/worktree-iso"

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
	run("add", "tracked.md")
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
	w.temp = t.TempDir()
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
	if after := retentionCheckpoint(t, st); !after.UserInterrupted || after.StopDirtyFiles == nil {
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

func TestResumeInterruptedHeadAdvanceWithoutIsolationFailsClosed(t *testing.T) {
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
		t.Fatalf("隔離記録なし統合のresumeがWorkerErrorになりません: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", st.TaskStatus())
	}
}

func stopTaskForIsolationGate(t *testing.T) *isolationGateFixture {
	t.Helper()
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.md"), []byte("作業中\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.StopGitSnapshot == nil {
		t.Fatal("停止時snapshotがありません")
	}
	return &isolationGateFixture{
		repo:       repo,
		st:         st,
		taskID:     st.ReadOr("task.id", ""),
		stopHead:   checkpoint.StopGitSnapshot.Head,
		checkpoint: checkpoint,
	}
}

func gitRetentionOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v失敗: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (f *isolationGateFixture) commitOnBranchAndMerge(t *testing.T, branch string) {
	t.Helper()
	tree := gitRetentionOutput(t, f.repo, "rev-parse", f.stopHead+"^{tree}")
	isolationCommit := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-p", f.stopHead, "-m", "isolation task result")
	runRetentionGit(t, f.repo, "update-ref", "refs/heads/"+branch, isolationCommit)
	runRetentionGit(t, f.repo, "merge", "--quiet", "--no-edit", branch)
}

func (f *isolationGateFixture) commitSourceChangeOnBranchAndMerge(t *testing.T, branch string) {
	t.Helper()
	runRetentionGit(t, f.repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(f.repo, "isolation-result.txt"), []byte("隔離成果\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, f.repo, "add", "isolation-result.txt")
	runRetentionGit(t, f.repo, "commit", "-q", "-m", "isolation task result")
	runRetentionGit(t, f.repo, "checkout", "-q", "-")
	runRetentionGit(t, f.repo, "merge", "--quiet", "--no-edit", branch)
	if got := gitRetentionOutput(t, f.repo, "diff", "--name-only", f.stopHead, "HEAD"); got != "isolation-result.txt" {
		t.Fatalf("統合後の停止時HEAD差 = %q want isolation-result.txtのみ", got)
	}
}

func (f *isolationGateFixture) advanceHeadWithoutBranch(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.repo, "manual.txt"), []byte("手動統合\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, f.repo, "add", "manual.txt")
	runRetentionGit(t, f.repo, "commit", "-q", "-m", "manual integration")
}

func (f *isolationGateFixture) saveRecord(t *testing.T, branch string, originTaskID string, originHead string) {
	t.Helper()
	if err := f.st.SaveIsolationRecord(state.IsolationRecord{
		IsolationID:    "iso-gate-1",
		Worktree:       isolationWorktreePath,
		Branch:         branch,
		CreatedAt:      "2026-08-25T00:00:00Z",
		OriginTaskID:   originTaskID,
		OriginRepoRoot: f.repo,
		OriginHead:     originHead,
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *isolationGateFixture) saveOrigin(t *testing.T, taskID string, branch string) {
	t.Helper()
	sibling := f.st.AttachSiblingStore(config.RepoHashFor(isolationWorktreePath))
	if err := os.MkdirAll(filepath.Dir(sibling.Path("repo-root")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := sibling.SaveIsolationOrigin(state.IsolationOrigin{
		IsolationID:    "iso-gate-1",
		OriginRepoRoot: f.repo,
		OriginTaskID:   taskID,
		Branch:         branch,
		CreatedAt:      "2026-08-25T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func expectRetentionFailure(t *testing.T, f *isolationGateFixture, wantIn string) {
	t.Helper()
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, f.st, resumeRunner, f.repo)
	err := resumeW.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("保持照合がWorkerErrorになりません: %v", err)
	}
	if !strings.Contains(workerErr.Message, wantIn) {
		t.Fatalf("fail closed理由 %q が %q を含みません", workerErr.Message, wantIn)
	}
	if f.st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("fail closed後のtask status = %s want interrupted", f.st.TaskStatus())
	}
}

func TestResumeInterruptedIsolatedIntegrationPasses(t *testing.T) {
	cases := []struct {
		name      string
		branch    string
		highRisk  bool
		integrate func(*isolationGateFixture, *testing.T, string)
	}{
		{
			name:   "metadata-only branch",
			branch: "glm-worker/isolation/isogate1",
			integrate: func(f *isolationGateFixture, t *testing.T, branch string) {
				f.commitOnBranchAndMerge(t, branch)
			},
		},
		{
			name:     "source-change branch",
			branch:   "glm-worker/isolation/isogate10",
			highRisk: true,
			integrate: func(f *isolationGateFixture, t *testing.T, branch string) {
				f.commitSourceChangeOnBranchAndMerge(t, branch)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := stopTaskForIsolationGate(t)
			tc.integrate(f, t, tc.branch)
			f.saveOrigin(t, f.taskID, tc.branch)
			f.saveRecord(t, tc.branch, f.taskID, f.stopHead)
			if f.checkpoint.StopDirtyFiles == nil {
				t.Fatal("停止時基準がありません")
			}

			steps := []runnerStep{
				{structured: implementedPacket("resumed")},
				{structured: passPacket()},
			}
			if tc.highRisk {
				steps = append(steps, runnerStep{structured: needsSolReviewPacket()})
			}
			resumeRunner := &scriptedRunner{steps: steps}
			resumeW := newGitWorkflowT(t, f.st, resumeRunner, f.repo)
			if err := resumeW.ExecuteResume(); err != nil {
				t.Fatalf("隔離branch統合後のresumeが保持照合を通過しません: %v", err)
			}
			wantStatus := state.TaskStatusComplete
			if tc.highRisk {
				wantStatus = state.TaskStatusWaitingSolReview
			}
			if f.st.TaskStatus() != wantStatus {
				t.Fatalf("task status = %s want %s", f.st.TaskStatus(), wantStatus)
			}
		})
	}
}

func TestResumeInterruptedIsolationAfterParentMetadataCommitPasses(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	if err := os.WriteFile(filepath.Join(f.repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, f.repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, f.repo, "commit", "-q", "-m", "parent metadata update")
	isolateHead := gitRetentionOutput(t, f.repo, "rev-parse", "HEAD")

	branch := "glm-worker/isolation/isogate2"
	tree := gitRetentionOutput(t, f.repo, "rev-parse", isolateHead+"^{tree}")
	isolationCommit := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-p", isolateHead, "-m", "isolation task result")
	runRetentionGit(t, f.repo, "update-ref", "refs/heads/"+branch, isolationCommit)
	runRetentionGit(t, f.repo, "merge", "--quiet", "--no-edit", branch)
	f.saveOrigin(t, f.taskID, branch)
	f.saveRecord(t, branch, f.taskID, isolateHead)

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, f.st, resumeRunner, f.repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("親metadata commitを挟んだ隔離統合後のresumeが通りません: %v", err)
	}
}

func TestResumeInterruptedIsolationPostIntegrationNonParentCommitFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate8"
	f.commitOnBranchAndMerge(t, branch)
	f.saveOrigin(t, f.taskID, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	f.advanceHeadWithoutBranch(t)
	expectRetentionFailure(t, f, "統合後に親管理外file")
}

func TestResumeInterruptedIsolationPostIntegrationParentMetadataCommitPasses(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate9"
	f.commitOnBranchAndMerge(t, branch)
	f.saveOrigin(t, f.taskID, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	if err := os.WriteFile(filepath.Join(f.repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, f.repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, f.repo, "commit", "-q", "-m", "parent metadata update")

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, f.st, resumeRunner, f.repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("統合後の親管理metadata commitを挟んだresumeが通りません: %v", err)
	}
}

func TestResumeInterruptedUnmergedIsolationBranchFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate3"
	tree := gitRetentionOutput(t, f.repo, "rev-parse", f.stopHead+"^{tree}")
	isolationCommit := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-p", f.stopHead, "-m", "isolation task result")
	runRetentionGit(t, f.repo, "update-ref", "refs/heads/"+branch, isolationCommit)
	f.advanceHeadWithoutBranch(t)
	f.saveOrigin(t, f.taskID, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	expectRetentionFailure(t, f, "統合されていません")
}

func TestResumeInterruptedIsolationRecordMismatchFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate4"
	f.commitOnBranchAndMerge(t, branch)
	f.saveOrigin(t, f.taskID, branch)

	f.saveRecord(t, branch, f.taskID+"-other", f.stopHead)
	expectRetentionFailure(t, f, "元task")

	if err := f.st.SaveIsolationRecord(state.IsolationRecord{
		IsolationID:    "iso-gate-1",
		Worktree:       isolationWorktreePath,
		Branch:         branch,
		OriginTaskID:   f.taskID,
		OriginRepoRoot: f.repo + "-elsewhere",
		OriginHead:     f.stopHead,
	}); err != nil {
		t.Fatal(err)
	}
	expectRetentionFailure(t, f, "元repo")
}

func TestResumeInterruptedIsolationBranchUnresolvableFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	f.advanceHeadWithoutBranch(t)
	f.saveOrigin(t, f.taskID, "glm-worker/isolation/gone")
	f.saveRecord(t, "glm-worker/isolation/gone", f.taskID, f.stopHead)
	expectRetentionFailure(t, f, "解決できません")
}

func TestResumeInterruptedIsolationOriginMissingFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate5"
	f.commitOnBranchAndMerge(t, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	expectRetentionFailure(t, f, "出自記録を読み込めません")
}

func TestResumeInterruptedIsolationOriginAsymmetryFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate6"
	f.commitOnBranchAndMerge(t, branch)

	runRetentionGit(t, f.repo, "branch", branch+"-other", f.stopHead)
	f.saveOrigin(t, f.taskID, branch)
	if err := f.st.SaveIsolationRecord(state.IsolationRecord{
		IsolationID:    "iso-gate-1",
		Worktree:       isolationWorktreePath,
		Branch:         branch + "-other",
		OriginTaskID:   f.taskID,
		OriginRepoRoot: f.repo,
		OriginHead:     f.stopHead,
	}); err != nil {
		t.Fatal(err)
	}
	expectRetentionFailure(t, f, "一致しません")
}

func TestResumeInterruptedIsolationOriginHeadDriftFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	f.advanceHeadWithoutBranch(t)

	tree := gitRetentionOutput(t, f.repo, "rev-parse", f.stopHead+"^{tree}")
	alienHead := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-m", "alien base")
	f.saveOrigin(t, f.taskID, "glm-worker/isolation/isogate7")
	f.saveRecord(t, "glm-worker/isolation/isogate7", f.taskID, alienHead)
	expectRetentionFailure(t, f, "作成HEADが停止時HEADと一致しません")
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
