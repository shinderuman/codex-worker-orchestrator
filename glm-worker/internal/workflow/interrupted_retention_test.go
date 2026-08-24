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

// newRetentionGitRepoは停止時保持基準の機械照合に必要な実git repositoryを用意する。
// git commandがない環境では保持照合test全体をskipする。
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
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
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

// newGitStateStoreTは実git repositoryをRepoRootに持つstate storeを用意する。
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
	return st
}

// newGitWorkflowTはcaptureSnapshot差替えをしない実repo向けworkflowを組む。
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

// stopWorkflowInCallは実行中呼出内で--stop要求を出してinterrupted保存まで進める。
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

// retentionCheckpointは保持照合対象のinterrupted checkpointを読む。
func retentionCheckpoint(t *testing.T, st *state.StateStore) state.ResumeCheckpoint {
	t.Helper()
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

// TestInterruptedStopCapturesRetentionは--stop停止保存が保持基準(snapshot・dirty列挙・
// recovery patch)をcheckpointとstateへ固定することを検証する。
func TestInterruptedStopCapturesRetention(t *testing.T) {
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644); err != nil {
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
		if file.Path == "uncommitted.txt" {
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

// TestResumeInterruptedUntouchedPassesは停止期間中に元checkoutへ誰も触れない場合の
// resume通過を固定する。
func TestResumeInterruptedUntouchedPasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644)
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

// TestResumeInterruptedParentMetadataDeltaPassesは停止期間中の親管理metadata編集
// (working treeのみ・HEAD不変)を承認してresumeすることを固定する。
func TestResumeInterruptedParentMetadataDeltaPasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v1\n"), 0o644)
	os.Mkdir(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "other-task.md"), []byte("task\n"), 0o644)

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("親管理metadata更新後のresumeが保持照合を通過しません: %v", err)
	}
}

// TestResumeInterruptedDirtyDriftFailsClosedは停止時に保持したdirty fileの内容変化・
// 新規dirty・消失をfail closedにすることを固定する。失敗時もstatusとcheckpointは
// interruptedのまま残る。
func TestResumeInterruptedDirtyDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())
	before := retentionCheckpoint(t, st)

	os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("衝突解決済み\n"), 0o644)
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

	// 停止時内容へ戻すと同じ--resumeが通過する。これがconflict recovery契約。この対象は
	// untracked fileのため停止patchではなく保持基準のhash照合で機械検証されており、本文の
	// 復元はtest(親Codex相当)が原本を保持していることによる。
	os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644)
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

// TestResumeInterruptedForeignDirtyFailsClosedは停止後に現れた新規の非親管理dirty fileを
// fail closedすることを固定する。
func TestResumeInterruptedForeignDirtyFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	os.WriteFile(filepath.Join(repo, "foreign.txt"), []byte("外部書込み\n"), 0o644)
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

// TestResumeInterruptedParentOnlyHeadAdvancePassesは停止期間中の親管理metadataだけを
// commitしたHEAD前進を承認することを固定する。
func TestResumeInterruptedParentOnlyHeadAdvancePasses(t *testing.T) {
	repo := newRetentionGitRepo(t)
	os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v1\n"), 0o644)
	runRetentionGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
	runRetentionGit(t, repo, "commit", "-q", "-m", "plan")
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("plan v2\n"), 0o644)
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

// TestResumeInterruptedHeadAdvanceWithoutIsolationFailsClosedは隔離記録がないまま
// 非親管理fileを含むHEAD前進をfail closedすることを固定する。
func TestResumeInterruptedHeadAdvanceWithoutIsolationFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	w := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, w, st, workerCheckpoint())

	os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644)
	runRetentionGit(t, repo, "add", "tracked.txt")
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

// isolationGateFixtureは隔離記録の実質検証へ必要な停止済みtaskの前提を揃える。
type isolationGateFixture struct {
	repo       string
	st         *state.StateStore
	taskID     string
	stopHead   string
	checkpoint state.ResumeCheckpoint
}

// stopTaskForIsolationGateはdirty作業を持つtaskを--stop停止まで進める。
func stopTaskForIsolationGate(t *testing.T) *isolationGateFixture {
	t.Helper()
	repo := newRetentionGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("作業中\n"), 0o644); err != nil {
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

// gitRetentionOutputはgit commandの出力をtrimして返す。
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

// isolationWorktreePathは実worktreeを伴わない記録検証testのworktree識別path。
const isolationWorktreePath = "/nonexistent/worktree-iso"

// commitOnBranchAndMergeはbranchを停止時HEADへ作り、隔離成果commit(checkout不要の同tree
// commit)を1つ乗せて元repoへmergeする。通常merge(fast-forward)なのでbranch tipは統合後HEAD
// の祖先に残る。
func (f *isolationGateFixture) commitOnBranchAndMerge(t *testing.T, branch string) {
	t.Helper()
	tree := gitRetentionOutput(t, f.repo, "rev-parse", f.stopHead+"^{tree}")
	isolationCommit := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-p", f.stopHead, "-m", "isolation task result")
	runRetentionGit(t, f.repo, "update-ref", "refs/heads/"+branch, isolationCommit)
	runRetentionGit(t, f.repo, "merge", "--quiet", "--no-edit", branch)
}

// advanceHeadWithoutBranchはbranchを経由しないHEAD前進(手動commit)を作る。
func (f *isolationGateFixture) advanceHeadWithoutBranch(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.repo, "manual.txt"), []byte("手動統合\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRetentionGit(t, f.repo, "add", "manual.txt")
	runRetentionGit(t, f.repo, "commit", "-q", "-m", "manual integration")
}

// saveRecordは元repo側隔離記録を保存する。taskID/repo/headは実値を使う。
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

// saveOriginは隔離worktree側stateへ対称な出自記録を保存する。
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

// expectRetentionFailureはresume保持照合がWorkerErrorでfail closedすることと、
// その時点でstatusがinterruptedのまま残ることを検証する。
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

// TestResumeInterruptedIsolatedIntegrationPassesは実branch統合と対称な出自記録を伴う
// 隔離記録のHEAD前進を、停止時dirty保持が崩れない限り承認することを固定する。
func TestResumeInterruptedIsolatedIntegrationPasses(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate1"
	f.commitOnBranchAndMerge(t, branch)
	f.saveOrigin(t, f.taskID, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	if f.checkpoint.StopDirtyFiles == nil {
		t.Fatal("停止時基準がありません")
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	resumeW := newGitWorkflowT(t, f.st, resumeRunner, f.repo)
	if err := resumeW.ExecuteResume(); err != nil {
		t.Fatalf("隔離統合後のresumeが保持照合を通過しません: %v", err)
	}
}

// TestResumeInterruptedIsolationAfterParentMetadataCommitPassesは停止→隔離の間の
// 親管理metadata commitを挟んだ隔離(作成HEADが停止時HEADの子孫)を承認することを固定する。
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

// TestResumeInterruptedUnmergedIsolationBranchFailsClosedは記録branchが統合される前に
// HEADが別経路で前進した場合をfail closedにすることを固定する。
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

// TestResumeInterruptedIsolationRecordMismatchFailsClosedは記録の元task・元repoが現在と
// 一致しない場合をfail closedにすることを固定する。
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

// TestResumeInterruptedIsolationBranchUnresolvableFailsClosedは記録branchが現在repoで
// 解決できない場合をfail closedにすることを固定する。
func TestResumeInterruptedIsolationBranchUnresolvableFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	f.advanceHeadWithoutBranch(t)
	f.saveOrigin(t, f.taskID, "glm-worker/isolation/gone")
	f.saveRecord(t, "glm-worker/isolation/gone", f.taskID, f.stopHead)
	expectRetentionFailure(t, f, "解決できません")
}

// TestResumeInterruptedIsolationOriginMissingFailsClosedは隔離worktree側stateの出自記録が
// 読めない場合をfail closedにすることを固定する。隔離側state dirは元task完了まで残す。
func TestResumeInterruptedIsolationOriginMissingFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate5"
	f.commitOnBranchAndMerge(t, branch)
	f.saveRecord(t, branch, f.taskID, f.stopHead)
	expectRetentionFailure(t, f, "出自記録を読み込めません")
}

// TestResumeInterruptedIsolationOriginAsymmetryFailsClosedは出自記録との対称性欠損を
// fail closedにすることを固定する。
func TestResumeInterruptedIsolationOriginAsymmetryFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	branch := "glm-worker/isolation/isogate6"
	f.commitOnBranchAndMerge(t, branch)
	// branch名だけ書き換えた非対称recordも解決可能な実branchを指すようにする。
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

// TestResumeInterruptedIsolationOriginHeadDriftFailsClosedは記録作成HEADが停止時HEADと
// 一致せず祖先関係もない場合をfail closedにすることを固定する。
func TestResumeInterruptedIsolationOriginHeadDriftFailsClosed(t *testing.T) {
	f := stopTaskForIsolationGate(t)
	f.advanceHeadWithoutBranch(t)
	// 停止時HEADを祖先に含まない孤立commitを作成HEADとして申告する。
	tree := gitRetentionOutput(t, f.repo, "rev-parse", f.stopHead+"^{tree}")
	alienHead := gitRetentionOutput(t, f.repo, "commit-tree", tree, "-m", "alien base")
	f.saveOrigin(t, f.taskID, "glm-worker/isolation/isogate7")
	f.saveRecord(t, "glm-worker/isolation/isogate7", f.taskID, alienHead)
	expectRetentionFailure(t, f, "作成HEADが停止時HEADと一致しません")
}

// TestResumeInterruptedNonAncestorHeadFailsClosedは停止時commitを祖先に含まない
// HEAD移動(amend・rebase等)をfail closedすることを固定する。
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

// TestResumeInterruptedLegacyCheckpointFailsClosedは保持基準を持たない旧形式
// interrupted checkpointのresumeをfail closedすることを固定する。
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
