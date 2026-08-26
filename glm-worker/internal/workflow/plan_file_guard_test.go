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

func newPlanFileWorkflow(t *testing.T, repoRoot string, steps []runnerStep, mutatePhase string, mutateSkipCalls int, mutate func(string) error) (*Workflow, *mutatingRunner, *bytes.Buffer, *state.StateStore) {
	t.Helper()
	st := newStateStoreT(t)
	r := &mutatingRunner{repoRoot: repoRoot, steps: steps, mutate: mutate, mutatePhase: mutatePhase, mutateSkipCalls: mutateSkipCalls}
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
	if got := len(r.prompts); got != wantCalls {
		t.Fatalf("model呼出回数 = %d want %d: %v", got, wantCalls, r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("task status = %q want waiting-sol-review", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("親管理metadata違反時にresume checkpointが残っています")
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
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "worker-new", 0, mutatePlanFile)
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	content, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile))
	if err != nil || string(content) != "glm edited plan\n" {
		t.Fatalf("GLMのplan変更がbaselineへ自動復元されています: %q %v", content, err)
	}
}

func TestPlanFileCreationDeletionAndUnchangedMatrix(t *testing.T) {
	cases := []struct {
		name string
		seed bool
		mutate func(string) error
		wantFail bool
		wantReason string
	}{
		{"absent creation", false, mutatePlanFile, true, "存在しない状態から新規作成"},
		{"deletion", true, func(root string) error { return os.Remove(filepath.Join(root, implementationPlanFile)) }, true, "削除されました"},
		{"unchanged", true, nil, false, ""},
		{"absent throughout", false, nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			if tc.seed { writePlanFileContent(t, repoRoot, planGuardSeed) }
			phase := ""
			if tc.mutate != nil { phase = "worker-new" }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, phase, 0, tc.mutate)
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			if tc.wantFail {
				requirePlanFileFailClosed(t, st, r, out, tc.wantReason, 1)
				return
			}
			if st.TaskStatus() != state.TaskStatusComplete || len(r.prompts) != 2 {
				t.Fatalf("通常flowを維持すべき: status=%q calls=%d", st.TaskStatus(), len(r.prompts))
			}
		})
	}
}

func TestPlanFileTrackedMissingFailsClosedBeforeCall(t *testing.T) {
	cases := map[string]func(t *testing.T, repoRoot string){
		"staged index entry": func(t *testing.T, repoRoot string) { gitIn(t, repoRoot, "add", implementationPlanFile) },
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
			if err := os.Remove(filepath.Join(repoRoot, implementationPlanFile)); err != nil { t.Fatal(err) }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}}, "", 0, nil)
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			if len(r.prompts) != 0 { t.Fatalf("追跡中plan欠損時はmodel呼出前に停止すべき: %d", len(r.prompts)) }
			if st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "working treeへ存在しません") {
				t.Fatalf("追跡中plan欠損をfail closedできていません: status=%q out=%s", st.TaskStatus(), out.String())
			}
		})
	}
}

func TestPlanFileTrackingIndeterminateFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	if err := os.Remove(filepath.Join(repoRoot, ".git", "HEAD")); err != nil { t.Fatal(err) }
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}}, "", 0, nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "Git追跡判定に失敗") {
		t.Fatalf("追跡判定不能を呼出前fail closedできていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
	}
}

func TestPlanFileUntrackedAbsentNonGitRepoProceeds(t *testing.T) {
	repoRoot := t.TempDir()
	w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "", 0, nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	if st.TaskStatus() != state.TaskStatusComplete || len(r.prompts) != 2 {
		t.Fatalf("git外repoのplan未追跡欠損は通常flowを維持すべき: status=%q calls=%d", st.TaskStatus(), len(r.prompts))
	}
}

func planFileDecisionWorkflow(t *testing.T, st *state.StateStore, repoRoot string, mutatePhase string, mutate func(string) error) (*Workflow, *mutatingRunner, *bytes.Buffer) {
	t.Helper()
	r := &mutatingRunner{repoRoot: repoRoot, steps: []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, mutate: mutate, mutatePhase: mutatePhase}
	out := &bytes.Buffer{}
	w := newMutationWorkflowShell(t, st)
	w.runner = r
	w.output = out
	w.config.RepoRoot = repoRoot
	w.captureSnapshot = state.CaptureGitSnapshot
	return w, r, out
}

func TestPlanFileDecisionAndExplicitFixMutationFailClosed(t *testing.T) {
	cases := []struct { name, status, phase string; run func(*Workflow) error }{
		{"decision", state.TaskStatusWaitingDecision, "worker-decision", func(w *Workflow) error { return w.ExecuteDecision("decision") }},
		{"explicit fix", state.TaskStatusWaitingSolReview, "worker-explicit-fix", func(w *Workflow) error { return w.ExecuteExplicitFix("fix instruction", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			st := newStateStoreT(t)
			if err := st.Write("last-request", "request"); err != nil { t.Fatal(err) }
			if tc.status == state.TaskStatusWaitingDecision { if err := st.Touch("pending-decision"); err != nil { t.Fatal(err) } }
			if err := st.SetTaskStatus(tc.status); err != nil { t.Fatal(err) }
			w, r, out := planFileDecisionWorkflow(t, st, repoRoot, tc.phase, mutatePlanFile)
			if err := tc.run(w); err != nil { t.Fatal(err) }
			requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
		})
	}
}

func TestPlanFileAutoFixMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: fixRequiredPacket()}, {structured: implementedPacket("fixed")}, {structured: passPacket()}}, "worker-auto-fix-1", 0, mutatePlanFile)
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 3)
}

func seedRateLimitedWorkerCheckpoint(t *testing.T, st *state.StateStore, request string) {
	t.Helper()
	if err := st.Write("last-request", request); err != nil { t.Fatal(err) }
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole, Model: "opus", Effort: "high", Prompt: "p", OriginalPrompt: "p", Request: request, RateLimited: true}); err != nil { t.Fatal(err) }
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil { t.Fatal(err) }
}

func TestPlanFileResumeMutationAndParentUpdateMatrix(t *testing.T) {
	t.Run("worker mutation", func(t *testing.T) {
		repoRoot := initMutationRepo(t)
		writePlanFileContent(t, repoRoot, planGuardSeed)
		st := newStateStoreT(t)
		seedRateLimitedWorkerCheckpoint(t, st, "request")
		w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-new", mutatePlanFile)
		if err := w.ExecuteResume(); err != nil { t.Fatal(err) }
		requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	})
	t.Run("parent update adopted", func(t *testing.T) {
		repoRoot := initMutationRepo(t)
		writePlanFileContent(t, repoRoot, planGuardSeed)
		st := newStateStoreT(t)
		seedRateLimitedWorkerCheckpoint(t, st, "request")
		parentUpdatedPlan := "# plan v2\n\n## ACTIVE\n\n- `" + activeTaskGuardPath + "`\n"
		writePlanFileContent(t, repoRoot, parentUpdatedPlan)
		w, r, _ := planFileDecisionWorkflow(t, st, repoRoot, "", nil)
		r.steps = []runnerStep{{structured: implementedPacket("resumed")}, {structured: passPacket()}}
		if err := w.ExecuteResume(); err != nil { t.Fatal(err) }
		content, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile))
		if err != nil || string(content) != parentUpdatedPlan || len(r.prompts) != 2 {
			t.Fatalf("親Codex更新内容をbaselineとして採用できていません: calls=%d content=%q err=%v", len(r.prompts), content, err)
		}
	})
}

func TestPlanFileTransientRecoveryResumedTaskMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}, {structured: implementedPacket("resumed")}, {structured: passPacket()}}, "worker-new", 0, mutatePlanFile)
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 2)
	if len(r.probes) != 1 { t.Fatalf("probe 1回の成功後にresumed taskで停止すべき: %d", len(r.probes)) }
}

func TestPlanFileReadErrorFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	if err := os.Mkdir(filepath.Join(repoRoot, implementationPlanFile), 0o755); err != nil { t.Fatal(err) }
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}}, "", 0, nil)
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "解決できません") {
		t.Fatalf("plan read errorを呼出前fail closedできていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
	}
}

const historyGuardSeed = "parent owned history\n"

func writeHistoryFileContent(t *testing.T, repoRoot string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, implementationHistoryFile), []byte(content), 0o644); err != nil { t.Fatal(err) }
}
func mutateHistoryFile(repoRoot string) error { return os.WriteFile(filepath.Join(repoRoot, implementationHistoryFile), []byte("glm edited history\n"), 0o644) }
func removeAndDirGuardFile(name string) func(string) error {
	return func(repoRoot string) error {
		if err := os.Remove(filepath.Join(repoRoot, name)); err != nil { return err }
		return os.Mkdir(filepath.Join(repoRoot, name), 0o755)
	}
}

func requireGuardTelemetryExactOnce(t *testing.T, st *state.StateStore, runnerCalls int, wantTaskOutcome, wantEventOutcome string) {
	t.Helper()
	taskRecords, eventRecords, total := 0, 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask { total++ }
		if l.CallType == state.CallTypeTask && l.Outcome == wantTaskOutcome { taskRecords++ }
		if l.CallType == state.CallTypeEvent && l.Outcome == wantEventOutcome { eventRecords++ }
	}
	if taskRecords != 1 || eventRecords != 1 || total != runnerCalls {
		t.Fatalf("telemetry task=%d event=%d total=%d want 1/1/%d", taskRecords, eventRecords, total, runnerCalls)
	}
	if stats := currentStats(t, st); stats.ModelCalls != runnerCalls { t.Fatalf("stats ModelCalls = %d want %d", stats.ModelCalls, runnerCalls) }
}

func TestParentMetadataAfterReadFailureRecordsExecutedCallOnce(t *testing.T) {
	for _, tc := range []struct { name, file string; seedHistory bool }{{"plan", implementationPlanFile, false}, {"history", implementationHistoryFile, true}} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			if tc.seedHistory { writeHistoryFileContent(t, repoRoot, historyGuardSeed) }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "worker-new", 0, removeAndDirGuardFile(tc.file))
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 1)
			requireGuardTelemetryExactOnce(t, st, 1, "parent_metadata_unavailable", "parent_metadata_unavailable")
		})
	}
}

func TestParentMetadataAfterReadFailureOnResumedTaskRecordsCallOnce(t *testing.T) {
	for _, tc := range []struct { name, file string; seedHistory bool }{{"plan", implementationPlanFile, false}, {"history", implementationHistoryFile, true}} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed)
			if tc.seedHistory { writeHistoryFileContent(t, repoRoot, historyGuardSeed) }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}, {structured: implementedPacket("resumed")}, {structured: passPacket()}}, "worker-new", 0, removeAndDirGuardFile(tc.file))
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			requirePlanFileFailClosed(t, st, r, out, "終了状態取得失敗", 2)
			if len(r.probes) != 1 { t.Fatalf("probe 1回の成功後にresumed taskで停止すべき: %d", len(r.probes)) }
			requireGuardTelemetryExactOnce(t, st, 2, "parent_metadata_unavailable", "parent_metadata_unavailable")
		})
	}
}

func TestReviewerMetadataMutationUsesExistingSnapshotInvariant(t *testing.T) {
	cases := []struct { name string; seed func(*testing.T, string); mutate func(string) error }{
		{"plan", func(t *testing.T, root string) { writePlanFileContent(t, root, planGuardSeed) }, mutatePlanFile},
		{"history", func(t *testing.T, root string) { writePlanFileContent(t, root, planGuardSeed); writeHistoryFileContent(t, root, historyGuardSeed) }, mutateHistoryFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t); tc.seed(t, repoRoot)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "", 0, tc.mutate)
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			if len(r.prompts) != 2 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "reviewer実行中にrepository状態が変化") {
				t.Fatalf("review-end snapshotで停止できていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
			}
		})
	}
}

func TestHistoryFileMutationMatrix(t *testing.T) {
	cases := []struct { name string; seedPlan, seedHistory bool; mutate func(string) error; wantFail bool; reason string }{
		{"worker mutation", true, true, mutateHistoryFile, true, "内容が変化"},
		{"absent creation with plan", true, false, mutateHistoryFile, true, "存在しない状態から新規作成"},
		{"deletion", true, true, func(root string) error { return os.Remove(filepath.Join(root, implementationHistoryFile)) }, true, "削除されました"},
		{"unchanged", true, true, nil, false, ""},
		{"absent untracked with plan", true, false, nil, false, ""},
		{"inactive without plan", false, false, mutateHistoryFile, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			if tc.seedPlan { writePlanFileContent(t, repoRoot, planGuardSeed) }
			if tc.seedHistory { writeHistoryFileContent(t, repoRoot, historyGuardSeed) }
			phase := ""; if tc.mutate != nil { phase = "worker-new" }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, phase, 0, tc.mutate)
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			if tc.wantFail { requirePlanFileFailClosed(t, st, r, out, tc.reason, 1); return }
			if st.TaskStatus() != state.TaskStatusComplete || len(r.prompts) != 2 { t.Fatalf("通常flowを維持すべき: status=%q calls=%d", st.TaskStatus(), len(r.prompts)) }
		})
	}
}

func TestHistoryFileTrackedMissingFailsClosedBeforeCall(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "staged"; if committed { name = "committed" }
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writePlanFileContent(t, repoRoot, planGuardSeed); writeHistoryFileContent(t, repoRoot, historyGuardSeed)
			gitIn(t, repoRoot, "add", implementationHistoryFile)
			if committed { gitIn(t, repoRoot, "commit", "-q", "-m", "add history") }
			if err := os.Remove(filepath.Join(repoRoot, implementationHistoryFile)); err != nil { t.Fatal(err) }
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}}, "", 0, nil)
			if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
			if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), implementationHistoryFile+"がworking treeへ存在しません") {
				t.Fatalf("追跡中history欠損をfail closedできていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
			}
		})
	}
}

func TestHistoryFileReadErrorFailsClosedBeforeCall(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	if err := os.Mkdir(filepath.Join(repoRoot, implementationHistoryFile), 0o755); err != nil { t.Fatal(err) }
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}}, "", 0, nil)
	if err := w.ExecuteNewTask("request"); err != nil { t.Fatal(err) }
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "親管理metadata baseline取得失敗") {
		t.Fatalf("history baseline取得失敗をfail closedできていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
	}
}

func TestHistoryFileResumeWorkerMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed); writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	st := newStateStoreT(t); seedRateLimitedWorkerCheckpoint(t, st, "request")
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "worker-new", mutateHistoryFile)
	if err := w.ExecuteResume(); err != nil { t.Fatal(err) }
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
}

func TestPlanFileGuardScenarioPinnedInEscapedCorpus(t *testing.T) {
	sc, mf := loadCorpus(t)
	found, trackedAbsent := "", ""
	for _, s := range sc.Scenarios {
		if s.WorkerMutatesPlanFile { found = s.ID }
		if s.PlanFileTrackedAbsent { trackedAbsent = s.ID }
	}
	if found == "" || trackedAbsent == "" { t.Fatalf("escaped corpus lacks plan guard scenarios: mutation=%q tracked-absent=%q", found, trackedAbsent) }
	for _, path := range []string{"AGENTS.md", "codex/glm-worker/prompts/WORKER.md"} {
		listed := false
		for _, e := range mf.InstructionFiles {
			if e.Path != path { continue }
			for _, sid := range e.Scenarios { if sid == found { listed = true } }
		}
		if !listed { t.Fatalf("manifestの%sが%sをpinしていません", path, found) }
	}
}

func TestPlanFileContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); if err != nil { t.Fatal(err) }
	for _, want := range []string{
		"tracked canonical source", "親Codexだけ", "読み取り専用", "更新候補と根拠をPACKETへ記載して親Codexへ報告",
		"Git indexで追跡されているのにworking treeへ存在しない場合と", "Git repository内で追跡判定自体ができない場合",
		"未追跡で最初から存在しない他repositoryおよびGit管理外directoryでは通常作業を許可", "親Codex専有のtracked archive",
		"編集・生成・削除を行わず", "全文を読まず必要な見出しだけを検索して読む", "planが存在するrepositoryでは",
		"planの無い旧repositoryとhistory未作成状態の通常作業は許可",
	} {
		if !strings.Contains(string(agents), want) { t.Errorf("root AGENTS.mdにplan/history契約文 %qがありません", want) }
	}
	managed, err := os.ReadFile(filepath.Join(root, "codex", "AGENTS.md")); if err != nil { t.Fatal(err) }
	if strings.Contains(string(managed), implementationPlanFile) { t.Error("global配布用codex/AGENTS.mdへrepository固有のplan規則を入れない") }
}
