package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	planGuardSeed       = "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/001-active.md`\n"
	activeTaskGuardSeed = "# ACTIVE task\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\n- guard検証用seed\n"
	activeTaskGuardPath = "IMPLEMENTATION_TASKS/001-active.md"
)

const historyGuardSeed = "parent owned history\n"

func writeActiveTaskFileContent(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoRoot, implementationTasksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(activeTaskGuardSeed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePlanFileContent(t *testing.T, repoRoot string, content string) {
	t.Helper()
	writeActiveTaskFileContent(t, repoRoot)
	if err := os.WriteFile(filepath.Join(repoRoot, implementationPlanFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newPlanFileWorkflow(
	t *testing.T,
	repoRoot string,
	steps []runnerStep,
	mutatePhase string,
	mutateSkipCalls int,
	mutate func(string) error,
) (*Workflow, *mutatingRunner, *bytes.Buffer, *state.StateStore) {
	t.Helper()
	st := newStateStoreT(t)
	r := &mutatingRunner{
		repoRoot:        repoRoot,
		steps:           steps,
		mutate:          mutate,
		mutatePhase:     mutatePhase,
		mutateSkipCalls: mutateSkipCalls,
	}
	out := &bytes.Buffer{}
	w := newMutationWorkflowShell(t, st)
	w.runner = r
	w.output = out
	w.config.RepoRoot = repoRoot
	w.captureSnapshot = state.CaptureGitSnapshot
	return w, r, out, st
}

func mutatePlanFile(repoRoot string) error {
	return os.WriteFile(filepath.Join(repoRoot, implementationPlanFile), []byte("glm edited plan\n"), 0o644)
}

func requirePlanFileFailClosed(t *testing.T, st *state.StateStore, r *mutatingRunner, out *bytes.Buffer, wantReason string, wantCalls int) {
	t.Helper()
	if got, want := len(r.prompts), wantCalls; got != want {
		t.Fatalf("model呼出回数 = %d want %d(次のreviewer開始前に停止すべき): %v", got, want, r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("plan file違反時にresume checkpointが残っています")
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
	if !strings.Contains(out.String(), wantReason) {
		t.Fatalf("fail closed理由 %qが出力されていません:\n%s", wantReason, out.String())
	}
}

func TestPlanFileWorkerMutationFailsClosedBeforeReview(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	if content, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile)); err != nil || string(content) != "glm edited plan\n" {
		t.Fatalf("GLMのplan変更がbaselineへ自動復元されています: %q %v", content, err)
	}
	taskViolations, mismatchEvents := 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == "parent_metadata_violation" {
			taskViolations++
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_mismatch" {
			mismatchEvents++
		}
	}
	if taskViolations != 1 || mismatchEvents != 1 {
		t.Fatalf("telemetry = task violation %d / mismatch event %d want 1/1", taskViolations, mismatchEvents)
	}
}

func TestPlanFileAbsentWorkerCreationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "存在しない状態から新規作成", 1)
}

func TestPlanFileDeletionFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, func(repoRoot string) error {
		return os.Remove(filepath.Join(repoRoot, implementationPlanFile))
	})

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "削除されました", 1)
}

func TestParentManagedMetadataNonGuardedStatesProceed(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*testing.T, string)
		session string
		mutate  func(string) error
	}{
		{
			name: "plan unchanged and history absent untracked",
			setup: func(t *testing.T, repoRoot string) {
				writePlanFileContent(t, repoRoot, planGuardSeed)
			},
		},
		{name: "plan absent throughout"},
		{
			name:    "history creation without plan",
			session: "worker-new",
			mutate:  mutateHistoryFile,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			if tc.setup != nil {
				tc.setup(t, repoRoot)
			}
			w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, tc.session, 0, tc.mutate)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != state.TaskStatusComplete {
				t.Fatalf("非guard状態は通常flowを維持すべき: %q", st.TaskStatus())
			}
			if len(r.prompts) != 2 {
				t.Fatalf("worker/reviewer 2呼出が必要: %d", len(r.prompts))
			}
		})
	}
}

func TestPlanFileTrackedMissingFailsClosedBeforeCall(t *testing.T) {
	cases := map[string]func(t *testing.T, repoRoot string){
		"staged index entry": func(t *testing.T, repoRoot string) {
			gitIn(t, repoRoot, "add", implementationPlanFile)
		},
		"committed tracked file": func(t *testing.T, repoRoot string) {
			gitIn(t, repoRoot, "add", implementationPlanFile)
			gitIn(t, repoRoot, "commit", "-q", "-m", "add plan")
		},
	}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			track(t, repoRoot)
			if err := os.Remove(filepath.Join(repoRoot, implementationPlanFile)); err != nil {
				t.Fatal(err)
			}
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 0 {
				t.Fatalf("追跡中plan欠損時はmodel呼出前に停止すべき: %d", len(r.prompts))
			}
			if st.TaskStatus() != state.TaskStatusWaitingSolReview {
				t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
			}
			if _, err := st.LoadResumeCheckpoint(); err == nil {
				t.Fatal("追跡中plan欠損のfail closed後にresume checkpointが残っています")
			}
			pkt := lastPacketFromOutput(t, out.String())
			if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
				t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
			}
			if !strings.Contains(out.String(), "working treeへ存在しません") {
				t.Fatalf("追跡中plan欠損理由が出力されていません:\n%s", out.String())
			}
			if _, err := os.Stat(filepath.Join(repoRoot, implementationPlanFile)); !os.IsNotExist(err) {
				t.Fatalf("欠損planが復元・生成されています: %v", err)
			}
			events := 0
			for _, l := range taskLogs(t, st) {
				if l.Outcome == "parent_metadata_missing" && strings.HasSuffix(l.Phase, parentMetadataGuardSurface.eventSuffix) {
					events++
				}
			}
			if events != 1 {
				t.Fatalf("parent_metadata_missing event = %d want 1", events)
			}
		})
	}
}

func TestPlanFileTrackingIndeterminateFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)

	if err := os.Remove(filepath.Join(repoRoot, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		return fixedSnapshot, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("追跡判定失敗時はmodel呼出前に停止すべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("追跡判定失敗のfail closed後にresume checkpointが残っています")
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
	if !strings.Contains(out.String(), "Git追跡判定に失敗") {
		t.Fatalf("追跡判定失敗理由が出力されていません:\n%s", out.String())
	}
	events := 0
	for _, l := range taskLogs(t, st) {
		if l.Outcome == "parent_metadata_unavailable" && strings.HasSuffix(l.Phase, parentMetadataGuardSurface.eventSuffix) {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("parent_metadata_unavailable event = %d want 1", events)
	}
}

func TestPlanFileUntrackedAbsentNonGitRepoProceeds(t *testing.T) {
	repoRoot := t.TempDir()
	w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		return fixedSnapshot, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("git外repoのplan未追跡欠損は通常flowを維持すべき: %q", st.TaskStatus())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer 2呼出が必要: %d", len(r.prompts))
	}
}

func TestPlanFileDecisionWorkerMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-decision", mutatePlanFile)

	if err := w.ExecuteDecision("decision"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
}

func TestPlanFileExplicitFixMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-explicit-fix", mutatePlanFile)

	if err := w.ExecuteExplicitFix("fix instruction", ""); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
}

func planFileDecisionWorkflow(t *testing.T, st *state.StateStore, repoRoot string, mutatePhase string, mutate func(string) error) (*Workflow, *mutatingRunner, *bytes.Buffer) {
	t.Helper()
	r := &mutatingRunner{
		repoRoot:    repoRoot,
		steps:       []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}},
		mutate:      mutate,
		mutatePhase: mutatePhase,
	}
	out := &bytes.Buffer{}
	w := newMutationWorkflowShell(t, st)
	w.runner = r
	w.output = out
	w.config.RepoRoot = repoRoot
	w.captureSnapshot = state.CaptureGitSnapshot
	return w, r, out
}

func TestPlanFileAutoFixMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: passPacket()},
	}, "worker-auto-fix-1", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 3)
}

func TestPlanFileResumeWorkerMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	st := newStateStoreT(t)
	seedRateLimitedWorkerCheckpoint(t, st, "request")
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-new", mutatePlanFile)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resume復元logicがfail closed状態を上書きしています: %q", st.TaskStatus())
	}
}

func TestPlanFileResumeAdoptsParentUpdateAsBaseline(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	st := newStateStoreT(t)
	seedRateLimitedWorkerCheckpoint(t, st, "request")

	parentUpdatedPlan := "# plan v2\n\n## ACTIVE\n\n- `" + activeTaskGuardPath + "`\n"
	writePlanFileContent(t, repoRoot, parentUpdatedPlan)
	w, r, _ := planFileDecisionWorkflow(t, st, repoRoot, "", nil)
	r.steps = []runnerStep{{structured: implementedPacket("resumed")}, {structured: passPacket()}}

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("親更新後のresumeは通常どおりworker/reviewerへ進むべき: %d", len(r.prompts))
	}
	if content, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile)); err != nil || string(content) != parentUpdatedPlan {
		t.Fatalf("親Codex更新内容が保持されていません: %q %v", content, err)
	}
}

func seedRateLimitedWorkerCheckpoint(t *testing.T, st *state.StateStore, request string) {
	t.Helper()
	if err := st.Write("last-request", request); err != nil {
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
		Request:        request,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
}

func TestPlanFileTransientRecoveryResumedTaskMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}, "worker-new", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 2)
	if len(r.probes) != 1 {
		t.Fatalf("probe 1回の成功後にresumed taskで停止すべき: %d", len(r.probes))
	}
}

func TestPlanFileReadErrorFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	if err := os.Mkdir(filepath.Join(repoRoot, implementationPlanFile), 0o755); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("baseline取得失敗時はmodel呼出前に停止すべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
	if !strings.Contains(out.String(), "解決できません") {
		t.Fatalf("ACTIVE task解決失敗理由が出力されていません:\n%s", out.String())
	}
}

func writeHistoryFileContent(t *testing.T, repoRoot string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, implementationHistoryFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateHistoryFile(repoRoot string) error {
	return os.WriteFile(filepath.Join(repoRoot, implementationHistoryFile), []byte("glm edited history\n"), 0o644)
}

func removeAndDirGuardFile(name string) func(string) error {
	return func(repoRoot string) error {
		if err := os.Remove(filepath.Join(repoRoot, name)); err != nil {
			return err
		}
		return os.Mkdir(filepath.Join(repoRoot, name), 0o755)
	}
}

func requireGuardTelemetryExactOnce(t *testing.T, st *state.StateStore, runnerCalls int, wantTaskOutcome string, wantEventOutcome string) {
	t.Helper()
	taskRecords, eventRecords := 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == wantTaskOutcome {
			taskRecords++
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == wantEventOutcome {
			eventRecords++
		}
	}
	if taskRecords != 1 {
		t.Fatalf("outcome %sのtask記録 = %d want 1(runner実行%d回)", wantTaskOutcome, taskRecords, runnerCalls)
	}
	if eventRecords != 1 {
		t.Fatalf("outcome %sのevent記録 = %d want 1", wantEventOutcome, eventRecords)
	}
	total := 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			total++
		}
	}
	if total != runnerCalls {
		t.Fatalf("task記録合計 = %d want %d", total, runnerCalls)
	}
	stats := currentStats(t, st)
	if stats.ModelCalls != runnerCalls {
		t.Fatalf("stats ModelCalls = %d want %d", stats.ModelCalls, runnerCalls)
	}
}

func TestPlanFileAfterReadFailureRecordsExecutedCallOnce(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, removeAndDirGuardFile(implementationPlanFile))

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 1)
	requireGuardTelemetryExactOnce(t, st, 1, "parent_metadata_unavailable", "parent_metadata_unavailable")
}

func TestHistoryFileAfterReadFailureRecordsExecutedCallOnce(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, removeAndDirGuardFile(implementationHistoryFile))

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 1)
	requireGuardTelemetryExactOnce(t, st, 1, "parent_metadata_unavailable", "parent_metadata_unavailable")
}

func TestPlanFileAfterReadFailureOnResumedTaskRecordsCallOnce(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}, "worker-new", 0, removeAndDirGuardFile(implementationPlanFile))

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 2)
	if len(r.probes) != 1 {
		t.Fatalf("probe 1回の成功後にresumed taskで停止すべき: %d", len(r.probes))
	}
	transient := 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == "transient_error" {
			transient++
		}
	}
	if transient != 1 {
		t.Fatalf("初回transient呼出の記録 = %d want 1", transient)
	}
	requireGuardTelemetryExactOnce(t, st, 2, "parent_metadata_unavailable", "parent_metadata_unavailable")
	stats := currentStats(t, st)
	if stats.TransientRetries != 1 {
		t.Fatalf("TransientRetries = %d want 1", stats.TransientRetries)
	}
}

func TestHistoryFileAfterReadFailureOnResumedTaskRecordsCallOnce(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}, "worker-new", 0, removeAndDirGuardFile(implementationHistoryFile))

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 2)
	if len(r.probes) != 1 {
		t.Fatalf("probe 1回の成功後にresumed taskで停止すべき: %d", len(r.probes))
	}
	requireGuardTelemetryExactOnce(t, st, 2, "parent_metadata_unavailable", "parent_metadata_unavailable")
}

func TestPlanFileReviewerMutationUsesExistingSnapshotInvariant(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("reviewer呼出まで進み既存invariantで停止すべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if !strings.Contains(out.String(), "reviewer実行中にrepository状態が変化") {
		t.Fatalf("reviewer変更はreview-end snapshot検出によるべきです:\n%s", out.String())
	}
	for _, l := range taskLogs(t, st) {
		if l.Outcome == "parent_metadata_mismatch" || l.Outcome == "parent_metadata_violation" {
			t.Fatalf("reviewer経路へplan guardが適用されています: %+v", l)
		}
	}
}

func TestHistoryFileWorkerMutationFailsClosedBeforeReview(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, mutateHistoryFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	if content, err := os.ReadFile(filepath.Join(repoRoot, implementationHistoryFile)); err != nil || string(content) != "glm edited history\n" {
		t.Fatalf("GLMのhistory変更がbaselineへ自動復元されています: %q %v", content, err)
	}
	if planContent, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile)); err != nil || string(planContent) != planGuardSeed {
		t.Fatalf("planは無変更のまま保たれるべき: %q %v", planContent, err)
	}
	taskViolations, mismatchEvents := 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == "parent_metadata_violation" {
			taskViolations++
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_mismatch" && strings.HasSuffix(l.Phase, parentMetadataGuardSurface.eventSuffix) {
			mismatchEvents++
		}
	}
	if taskViolations != 1 || mismatchEvents != 1 {
		t.Fatalf("telemetry = task violation %d / mismatch event %d want 1/1", taskViolations, mismatchEvents)
	}
}

func TestHistoryFileAbsentWorkerCreationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, mutateHistoryFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "存在しない状態から新規作成", 1)
}

func TestHistoryFileDeletionFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "worker-new", 0, func(repoRoot string) error {
		return os.Remove(filepath.Join(repoRoot, implementationHistoryFile))
	})

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "削除されました", 1)
}

func TestHistoryFileUnchangedProceedsToReview(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("history不変時は通常reviewを通ってcompleteになるべき: %q", st.TaskStatus())
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer 2呼出が必要: %d", len(r.prompts))
	}
}

func TestHistoryFileTrackedMissingFailsClosedBeforeCall(t *testing.T) {
	cases := map[string]func(t *testing.T, repoRoot string){
		"staged index entry": func(t *testing.T, repoRoot string) {
			gitIn(t, repoRoot, "add", implementationHistoryFile)
		},
		"committed tracked file": func(t *testing.T, repoRoot string) {
			gitIn(t, repoRoot, "add", implementationHistoryFile)
			gitIn(t, repoRoot, "commit", "-q", "-m", "add history")
		},
	}
	for name, track := range cases {
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			writeHistoryFileContent(t, repoRoot, historyGuardSeed)
			track(t, repoRoot)
			if err := os.Remove(filepath.Join(repoRoot, implementationHistoryFile)); err != nil {
				t.Fatal(err)
			}
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 0 {
				t.Fatalf("追跡中history欠損時はmodel呼出前に停止すべき: %d", len(r.prompts))
			}
			if st.TaskStatus() != state.TaskStatusWaitingSolReview {
				t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
			}
			if _, err := st.LoadResumeCheckpoint(); err == nil {
				t.Fatal("追跡中history欠損のfail closed後にresume checkpointが残っています")
			}
			pkt := lastPacketFromOutput(t, out.String())
			if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
				t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
			}
			if !strings.Contains(out.String(), implementationHistoryFile+"がworking treeへ存在しません") {
				t.Fatalf("追跡中history欠損理由が出力されていません:\n%s", out.String())
			}
			if _, err := os.Stat(filepath.Join(repoRoot, implementationHistoryFile)); !os.IsNotExist(err) {
				t.Fatalf("欠損historyが復元・生成されています: %v", err)
			}
			events := 0
			for _, l := range taskLogs(t, st) {
				if l.CallType == state.CallTypeTask {
					t.Fatalf("呼出前停止なのにtask記録が残っています: %+v", l)
				}
				if l.Outcome == "parent_metadata_missing" && strings.HasSuffix(l.Phase, parentMetadataGuardSurface.eventSuffix) {
					events++
				}
			}
			if events != 1 {
				t.Fatalf("parent_metadata_missing event = %d want 1", events)
			}
		})
	}
}

func TestHistoryFileReadErrorFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	if err := os.Mkdir(filepath.Join(repoRoot, implementationHistoryFile), 0o755); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("history baseline取得失敗時はmodel呼出前に停止すべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
	if !strings.Contains(out.String(), "親管理metadata baseline取得失敗") {
		t.Fatalf("親管理metadata baseline取得失敗理由が出力されていません:\n%s", out.String())
	}
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			t.Fatalf("呼出前停止なのにtask記録が残っています: %+v", l)
		}
	}
}

func TestHistoryFileResumeWorkerMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	st := newStateStoreT(t)
	seedRateLimitedWorkerCheckpoint(t, st, "request")
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-new", mutateHistoryFile)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resume復元logicがfail closed状態を上書きしています: %q", st.TaskStatus())
	}
}

func TestHistoryFileReviewerMutationUsesExistingSnapshotInvariant(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, mutateHistoryFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("reviewer呼出まで進み既存invariantで停止すべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if !strings.Contains(out.String(), "reviewer実行中にrepository状態が変化") {
		t.Fatalf("reviewer変更はreview-end snapshot検出によるべきです:\n%s", out.String())
	}
	for _, l := range taskLogs(t, st) {
		if l.Outcome == "parent_metadata_mismatch" || l.Outcome == "parent_metadata_violation" {
			t.Fatalf("reviewer経路へhistory guardが適用されています: %+v", l)
		}
	}
}

func TestPlanFileContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"tracked canonical source",
		"親Codexだけ",
		"読み取り専用",
		"更新候補と根拠をPACKETへ記載して親Codexへ報告",
		"Git indexで追跡されているのにworking treeへ存在しない場合と",
		"Git repository内で追跡判定自体ができない場合",
		"未追跡で最初から存在しない他repositoryおよびGit管理外directoryでは通常作業を許可",
		"bounded exceptional record",
		"通常task完了時にHistoryを読んだり追記したりしない",
		"GLM worker/reviewerは編集・生成・削除せず",
		"ACTIVE taskが明示参照した見出しだけを読む",
		"Historyへの通常完了移行は行わない",
	} {
		if !strings.Contains(string(agents), want) {
			t.Errorf("root AGENTS.mdにplan/history契約文 %qがありません", want)
		}
	}
	managed, err := os.ReadFile(filepath.Join(root, "codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(managed), implementationPlanFile) {
		t.Error("global配布用codex/AGENTS.mdへrepository固有のplan規則を入れない")
	}
}
