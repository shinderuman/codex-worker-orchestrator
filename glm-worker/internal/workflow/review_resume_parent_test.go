package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type reviewResumeDeltaCase struct {
	name              string
	planAtReviewStart string
	planAtStop        string
	planAtResume      string
	taskAtReviewStart string
	taskAtStop        string
	taskAtResume      string
	mutateCurrent     func(snap *state.GitSnapshot)
}

type reviewResumeDeltaRun struct {
	store  *state.StateStore
	runner *scriptedRunner
	output *bytes.Buffer
}

const activeTaskRepoPath = state.ParentTasksDir + "/001-active.md"

func writeRepoParentPlan(t *testing.T, repoRoot string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, state.ParentPlanFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRepoActiveTask(t *testing.T, repoRoot string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoRoot, state.ParentTasksDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskRepoPath), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func removeRepoActiveTask(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repoRoot, activeTaskRepoPath)); err != nil {
		t.Fatal(err)
	}
}

func removeRepoParentPlan(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repoRoot, state.ParentPlanFile)); err != nil {
		t.Fatal(err)
	}
}

func repoParentStates(t *testing.T, repoRoot string) state.ParentFileStates {
	t.Helper()
	states, err := state.CaptureParentFileStates(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func parentStateForContent(path string, content string) state.ParentFileState {
	if content == "" {
		return state.ParentFileState{Path: path}
	}
	sum := sha256.Sum256([]byte(content))
	return state.ParentFileState{Path: path, Exists: true, SHA256: hex.EncodeToString(sum[:])}
}

func makeStopStates(planContent string, taskContent string) state.ParentFileStates {
	return state.ParentFileStates{
		parentStateForContent(state.ParentPlanFile, planContent),
		parentStateForContent(activeTaskRepoPath, taskContent),
	}
}

func reviewResumeSnapshot(worktree string, excluding string, parents *state.ParentFileStates) state.GitSnapshot {
	return state.GitSnapshot{
		Head:                          "head-1",
		IndexDigest:                   "index-1",
		WorktreeDigest:                worktree,
		WorktreeDigestExcludingParent: excluding,
		ParentFiles:                   parents,
	}
}

func reviewResumeCheckpoint(stop *state.ParentFileStates) state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           "reviewer-1",
		Role:            state.ReviewerRole,
		Model:           "sonnet",
		ReadOnly:        true,
		Effort:          "high",
		Prompt:          "review",
		OriginalPrompt:  "review",
		Request:         "request",
		WorkerResult:    workerResultFromBody(workerPacket()),
		ReviewNumber:    1,
		RateLimited:     true,
		StopParentFiles: stop,
	}
}

func seedReviewResumeStop(t *testing.T, st *state.StateStore, saved state.GitSnapshot, checkpoint state.ResumeCheckpoint) {
	t.Helper()
	if err := st.SaveReviewStartSnapshot(saved); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
}

func newReviewResumeWorkflow(t *testing.T, st *state.StateStore, r *scriptedRunner, out io.Writer) *Workflow {
	t.Helper()
	w := newWorkflowT(t, st, r)
	w.output = out
	w.captureBoundarySnapshot = func(repoRoot string) (state.GitSnapshot, error) {
		snapshot, err := w.captureSnapshot(repoRoot)
		if err != nil {
			return snapshot, err
		}
		parents, err := state.CaptureParentFileStates(repoRoot)
		if err != nil {
			return snapshot, err
		}
		snapshot.ParentFiles = &parents
		return snapshot, nil
	}
	return w
}

func assertReviewResumeStopped(t *testing.T, st *state.StateStore, r *scriptedRunner, out *bytes.Buffer) {
	t.Helper()
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if len(r.prompts) != 0 {
		t.Fatalf("reviewerを呼ばず停止すべき: calls=%d", len(r.prompts))
	}
	if !strings.Contains(out.String(), "review開始時から状態が変化") {
		t.Fatalf("resume drift原因がpacketへ出力されていません: %q", out.String())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("fail closed後はcheckpointを残さない")
	}
}

func TestReviewResumeParentUpdateAcceptedReanchorsBaseline(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out bytes.Buffer
	w := newReviewResumeWorkflow(t, st, r, &out)
	repoRoot := w.config.RepoRoot
	writeRepoParentPlan(t, repoRoot, "plan-at-review-start\n")
	base := repoParentStates(t, repoRoot)
	seedReviewResumeStop(t, st, reviewResumeSnapshot("worktree-0", "excluding-1", &base), reviewResumeCheckpoint(&base))

	writeRepoParentPlan(t, repoRoot, "plan-parent-updated-during-stop\n")
	current := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return current, nil }

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q want complete", st.TaskStatus())
	}
	if len(r.prompts) != 1 {
		t.Fatalf("reviewer resumeが1回だけ呼ばれるべき: calls=%d", len(r.prompts))
	}
	reanchored, err := st.LoadReviewStartSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if reanchored.WorktreeDigest != "worktree-1" {
		t.Fatalf("review基準が現状へ再固定されていません: %#v", reanchored)
	}
	wantParents := repoParentStates(t, repoRoot)
	if reanchored.ParentFiles == nil || !state.SameParentFileStates(*reanchored.ParentFiles, wantParents) {
		t.Fatalf("再固定後の親管理file基準 = %#v want %#v", reanchored.ParentFiles, wantParents)
	}
	comparison, err := st.LoadSnapshotComparison()
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Stage != state.SnapshotStageReviewEnd || !comparison.Matched {
		t.Fatalf("再固定基準へのreview-end一致比較が記録されるべき: %#v", comparison)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range logs {
		if entry.Outcome == "snapshot_parent_update" && entry.Phase == "reviewer-1-review-resume-parent-update" {
			found = true
		}
	}
	if !found {
		t.Fatalf("承認再固定のtelemetry eventが記録されていません: %d entries", len(logs))
	}
}

func runReviewResumeDeltaCase(t *testing.T, tt reviewResumeDeltaCase) reviewResumeDeltaRun {
	t.Helper()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	out := &bytes.Buffer{}
	w := newReviewResumeWorkflow(t, st, r, out)
	repoRoot := w.config.RepoRoot
	if tt.planAtReviewStart != "" {
		writeRepoParentPlan(t, repoRoot, tt.planAtReviewStart)
	}
	if tt.taskAtReviewStart != "" {
		writeRepoActiveTask(t, repoRoot, tt.taskAtReviewStart)
	}
	reviewStartParents := repoParentStates(t, repoRoot)
	stop := makeStopStates(tt.planAtStop, tt.taskAtStop)
	seedReviewResumeStop(t, st, reviewResumeSnapshot("worktree-0", "excluding-1", &reviewStartParents), reviewResumeCheckpoint(&stop))

	if tt.planAtResume == "" {
		removeRepoParentPlan(t, repoRoot)
	} else {
		writeRepoParentPlan(t, repoRoot, tt.planAtResume)
	}
	switch {
	case tt.taskAtResume == "":
		if tt.taskAtReviewStart != "" {
			removeRepoActiveTask(t, repoRoot)
		}
	case tt.taskAtReviewStart == "":
		writeRepoActiveTask(t, repoRoot, tt.taskAtResume)
	default:
		writeRepoActiveTask(t, repoRoot, tt.taskAtResume)
	}
	current := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
	if tt.mutateCurrent != nil {
		tt.mutateCurrent(&current)
	}
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return current, nil }

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	return reviewResumeDeltaRun{store: st, runner: r, output: out}
}

func assertReviewResumeAccepted(t *testing.T, run reviewResumeDeltaRun) {
	t.Helper()
	if run.store.TaskStatus() != state.TaskStatusComplete || len(run.runner.prompts) != 1 {
		t.Fatalf("承認済み親更新はreviewer再開を許可するべき: status=%q calls=%d", run.store.TaskStatus(), len(run.runner.prompts))
	}
}

func TestReviewResumeParentUpdatesDuringStopAreAccepted(t *testing.T) {
	tests := []reviewResumeDeltaCase{
		{
			name:              "parent content update during stop accepted",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p1\n",
		},
		{
			name:         "parent creation during stop accepted",
			planAtResume: "p1\n",
		},
		{
			name:              "active task file content update during stop accepted",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p0\n",
			taskAtReviewStart: "t0\n",
			taskAtStop:        "t0\n",
			taskAtResume:      "t1\n",
		},
		{
			name:              "active task file creation during stop accepted",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p0\n",
			taskAtResume:      "t1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertReviewResumeAccepted(t, runReviewResumeDeltaCase(t, tt))
		})
	}
}

func TestReviewResumeRepositoryDriftDuringStopIsRejected(t *testing.T) {
	tests := []reviewResumeDeltaCase{
		{
			name:              "other path change during stop rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p1\n",
			mutateCurrent:     func(snap *state.GitSnapshot) { snap.WorktreeDigestExcludingParent = "excluding-2" },
		},
		{
			name:              "head move during stop rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p1\n",
			mutateCurrent:     func(snap *state.GitSnapshot) { snap.Head = "head-2" },
		},
		{
			name:              "index move during stop rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p1\n",
			mutateCurrent:     func(snap *state.GitSnapshot) { snap.IndexDigest = "index-2" },
		},
		{
			name:              "parent deletion during stop rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
		},
		{
			name:              "active task file deletion during stop rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p0\n",
			taskAtReviewStart: "t0\n",
			taskAtStop:        "t0\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runReviewResumeDeltaCase(t, tt)
			assertReviewResumeStopped(t, run.store, run.runner, run.output)
		})
	}
}

func TestReviewResumeReviewerMutationIsRejected(t *testing.T) {
	tests := []reviewResumeDeltaCase{
		{
			name:              "reviewer change during call rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p1\n",
			planAtResume:      "p1\n",
		},
		{
			name:              "reviewer change plus stop-period change on same file rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p1\n",
			planAtResume:      "p2\n",
		},
		{
			name:         "creation during reviewer call then stop-period change rejected",
			planAtStop:   "p1\n",
			planAtResume: "p2\n",
		},
		{
			name:              "active task file reviewer change plus stop-period change rejected",
			planAtReviewStart: "p0\n",
			planAtStop:        "p0\n",
			planAtResume:      "p0\n",
			taskAtReviewStart: "t0\n",
			taskAtStop:        "t1\n",
			taskAtResume:      "t2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runReviewResumeDeltaCase(t, tt)
			assertReviewResumeStopped(t, run.store, run.runner, run.output)
		})
	}
}

func TestReviewResumeLegacyStateFailsClosed(t *testing.T) {
	tests := []struct {
		name           string
		legacyStop     bool
		legacySnapshot bool
	}{
		{name: "legacy checkpoint without stop parent states", legacyStop: true},
		{name: "legacy snapshot without excluding digest", legacySnapshot: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
			var out bytes.Buffer
			w := newReviewResumeWorkflow(t, st, r, &out)
			repoRoot := w.config.RepoRoot
			writeRepoParentPlan(t, repoRoot, "p0\n")
			base := repoParentStates(t, repoRoot)
			saved := reviewResumeSnapshot("worktree-0", "excluding-1", &base)
			stop := base
			if tt.legacyStop {
				stop = state.ParentFileStates{}
			}
			checkpoint := reviewResumeCheckpoint(&stop)
			if tt.legacyStop {
				checkpoint.StopParentFiles = nil
			}
			if tt.legacySnapshot {
				saved.WorktreeDigestExcludingParent = ""
				saved.ParentFiles = nil
			}
			seedReviewResumeStop(t, st, saved, checkpoint)

			writeRepoParentPlan(t, repoRoot, "p1\n")
			current := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
			w.captureSnapshot = func(string) (state.GitSnapshot, error) { return current, nil }

			if err := w.ExecuteResume(); err != nil {
				t.Fatal(err)
			}
			assertReviewResumeStopped(t, st, r, &out)
		})
	}
}

func TestReviewResumeParentUpdateThenReviewerMutationFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out bytes.Buffer
	w := newReviewResumeWorkflow(t, st, r, &out)
	repoRoot := w.config.RepoRoot
	writeRepoParentPlan(t, repoRoot, "p0\n")
	base := repoParentStates(t, repoRoot)
	seedReviewResumeStop(t, st, reviewResumeSnapshot("worktree-0", "excluding-1", &base), reviewResumeCheckpoint(&base))

	writeRepoParentPlan(t, repoRoot, "p1\n")
	queue := &queueCapturer{results: []captureResult{
		{snap: reviewResumeSnapshot("worktree-1", "excluding-1", nil)},
		{snap: reviewResumeSnapshot("worktree-2", "excluding-1", nil)},
	}}
	w.captureSnapshot = queue.capture

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if len(r.prompts) != 1 {
		t.Fatalf("reviewerは1回呼ばれた後で停止するべき: calls=%d", len(r.prompts))
	}
	if !strings.Contains(out.String(), "reviewer実行中にrepository状態が変化") {
		t.Fatalf("review-end再照合の原因がpacketへ出力されていません: %q", out.String())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("review-end fail closed後はcheckpointを残さない")
	}
}

func TestFailedResumeCallRecapturesStopParentStates(t *testing.T) {
	st := newStateStoreT(t)
	var out bytes.Buffer
	w := newReviewResumeWorkflow(t, st, nil, &out)
	repoRoot := w.config.RepoRoot
	r := &scriptedRunner{steps: []runnerStep{{runErr: errors.New("boom")}}}
	r.onRun = func() { writeRepoParentPlan(t, repoRoot, "reviewer-edit\n") }
	w.runner = r
	writeRepoParentPlan(t, repoRoot, "p0\n")
	base := repoParentStates(t, repoRoot)
	seedReviewResumeStop(t, st, reviewResumeSnapshot("worktree-0", "excluding-1", &base), reviewResumeCheckpoint(&base))

	matched := reviewResumeSnapshot("worktree-0", "excluding-1", nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return matched, nil }

	if err := w.ExecuteResume(); err == nil {
		t.Fatal("plain error resumeはerrorを返す")
	}
	restored, err := st.LoadResumeCheckpoint()
	if err != nil || !restored.RateLimited {
		t.Fatalf("失敗resume後もrate-limited checkpointが復元されるべき: %v", err)
	}
	wantStop := repoParentStates(t, repoRoot)
	if restored.StopParentFiles == nil || !state.SameParentFileStates(*restored.StopParentFiles, wantStop) {
		t.Fatalf("復元checkpointの停止時親stateが呼出後時点へ固定されていません: %#v", restored.StopParentFiles)
	}

	r2 := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out2 bytes.Buffer
	w2 := newReviewResumeWorkflow(t, st, r2, &out2)
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	current := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
	w2.captureSnapshot = func(string) (state.GitSnapshot, error) { return current, nil }

	if err := w2.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	assertReviewResumeStopped(t, st, r2, &out2)
}

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
		if observed.StopParentFiles != nil {
			t.Errorf("pre-call保存が停止時親state基準を持ち越しています: %#v", observed.StopParentFiles)
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
	if crashed.StopParentFiles != nil {
		t.Fatalf("crash残存checkpointが停止時親state基準を保持している: %#v", crashed.StopParentFiles)
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

	crashed.RateLimited = true
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
		RateLimited:    true,
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
	if err != nil || !cp.RateLimited {
		t.Fatalf("resume checkpointがrate-limitedで保存されていません: %v", err)
	}
	want := repoParentStates(t, w.config.RepoRoot)
	if cp.StopParentFiles == nil || !state.SameParentFileStates(*cp.StopParentFiles, want) {
		t.Fatalf("停止時親管理file状態がcheckpointへ記録されていません: %#v want %#v", cp.StopParentFiles, want)
	}
}
