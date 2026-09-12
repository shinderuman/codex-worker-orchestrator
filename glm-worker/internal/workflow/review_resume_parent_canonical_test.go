package workflow

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewResumeCrashWindowTamperFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out bytes.Buffer
	w := newReviewResumeWorkflow(t, st, r, &out)
	repoRoot := w.config.RepoRoot
	writeRepoParentPlan(t, repoRoot, "p0\n")
	base := repoParentStates(t, repoRoot)
	seedReviewResumeStop(t, st, reviewResumeSnapshot("worktree-0", "excluding-1", &base), reviewResumeCheckpoint(&base))

	writeRepoParentPlan(t, repoRoot, "p1\n")
	r.onRun = func() {
		observed, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Errorf("呼出開始時点のcheckpoint読込: %v", err)
			return
		}
		if observed.StopGitSnapshot != nil {
			t.Errorf("pre-call保存が停止時repository boundaryを持ち越しています: %#v", observed.StopGitSnapshot)
		}
		writeRepoParentPlan(t, repoRoot, "reviewer-tamper-during-call\n")
		panic("simulated crash mid-call")
	}
	accepted := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return accepted, nil }
	func() {
		defer func() { _ = recover() }()
		_ = w.ExecuteResume()
	}()

	crashed, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if crashed.StopGitSnapshot != nil {
		t.Fatalf("crash残存checkpointが停止時repository boundaryを保持している: %#v", crashed.StopGitSnapshot)
	}
	if st.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("crash前のreviewer完了は無い前提")
	}

	tampered := reviewResumeSnapshot("worktree-2", "excluding-1", nil)
	var out2 bytes.Buffer
	w2 := newReviewResumeWorkflow(t, st, &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}, &out2)
	w2.captureSnapshot = func(string) (state.GitSnapshot, error) { return tampered, nil }
	if err := w2.ExecuteResume(); err == nil || !strings.Contains(err.Error(), "not stopped") {
		t.Fatalf("crash残存checkpointの直接resumeはgate errorになるべき: %v", err)
	}

	crashed.StopKind = state.ResumeStopRateLimited
	if err := st.SaveResumeCheckpoint(crashed); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r3 := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out3 bytes.Buffer
	w3 := newReviewResumeWorkflow(t, st, r3, &out3)
	w3.captureSnapshot = func(string) (state.GitSnapshot, error) { return tampered, nil }
	if err := w3.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	assertReviewResumeStopped(t, st, r3, &out3)
}

func TestWorkerResumeParentUpdateDuringStopProceeds(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	repoRoot := w.config.RepoRoot
	writeRepoActiveTask(t, repoRoot, "task-at-stop\n\n## External feasibility\n\nstatus: not-applicable\n")
	writeRepoParentPlan(t, repoRoot, "# plan\n\n## ACTIVE\n\n- `"+activeTaskRepoPath+"`\n")
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "req",
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	writeRepoParentPlan(t, repoRoot, "# plan v2\n\n## ACTIVE\n\n- `"+activeTaskRepoPath+"`\n")

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q want complete", st.TaskStatus())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker+reviewerが呼ばれるべき: calls=%d", len(r.prompts))
	}
}

func TestRateLimitStopRecordsStopParentFiles(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		output: zaiFiveHourLog,
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, r)
	w.config.RepoShort = "testrepo1234"
	w.temp = t.TempDir()
	writeRepoParentPlan(t, w.config.RepoRoot, "plan-at-stop\n")

	var limitErr runner.ZaiRateLimitError
	if _, err := w.runModel(reviewResumeCheckpoint(nil)); err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	cp, err := st.LoadResumeCheckpoint()
	if err != nil || cp.StopKind != state.ResumeStopRateLimited {
		t.Fatalf("resume checkpointがrate-limitedで保存されていません: %v", err)
	}
	want := repoParentStates(t, w.config.RepoRoot)
	if cp.StopGitSnapshot == nil || cp.StopGitSnapshot.ParentFiles == nil || !state.SameParentFileStates(*cp.StopGitSnapshot.ParentFiles, want) {
		t.Fatalf("停止時親管理file状態がcanonical snapshotへ記録されていません: %#v want %#v", cp.StopGitSnapshot, want)
	}
}
