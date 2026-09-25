package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunModelSurfacesZaiFiveHourLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		output: zaiFiveHourLog,
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, r)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "p",
		Request: "req",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	taskID, taskErr := st.TaskID()
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if limitErr.TaskID != taskID || limitErr.RepoRoot != "/repo" {
		t.Fatalf("rate limit errorのtask/repo = %q/%q want %q//repo", limitErr.TaskID, limitErr.RepoRoot, taskID)
	}
	available, resumeAt := limitErr.AutoResumeSchedule()
	if !available || resumeAt != "2026-07-22T14:08:34+08:00" {
		t.Fatalf("auto-resume schedule = %v/%q", available, resumeAt)
	}
	if key := limitErr.AutoResumeKey(); key != "glm-worker-resume-testrepo1234-"+taskID[:8] {
		t.Fatalf("auto-resume key = %q", key)
	}

	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || cp.StopKind != state.ResumeStopRateLimited {
		t.Fatalf("resume checkpointがrate-limitedで保存されていません: %v", cerr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 1 || logs[0].Outcome != "rate_limited" {
		t.Fatalf("rate limit telemetry = %#v", logs)
	}
}

func TestRunModelSurfacesPlainStdoutFiveHourLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		runErr: errors.New("exit status 1"),
		result: runner.RunResult{PlainFailure: runner.ProviderFailureClass{
			Kind:          runner.ProviderFailureZaiFiveHour,
			FiveHourLimit: runner.ZaiFiveHourLimit{ResetAtRFC3339: "2026-07-22T14:06:34+08:00"},
		}},
	}}}
	w := newWorkflowT(t, st, r)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "p",
		Request: "req",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestMergePlainFailureClassPriority(t *testing.T) {
	fiveHour := runner.ProviderFailureClass{Kind: runner.ProviderFailureZaiFiveHour}
	transientFile := runner.ProviderFailureClass{Kind: runner.ProviderFailureTransient, Detail: "network:dial tcp"}
	transientPlain := runner.ProviderFailureClass{Kind: runner.ProviderFailureTransient, Detail: "http-503"}
	fatal := runner.ProviderFailureClass{Kind: runner.ProviderFailureFatal}
	empty := runner.ProviderFailureClass{}

	cases := []struct {
		name  string
		base  runner.ProviderFailureClass
		plain runner.ProviderFailureClass
		want  runner.ProviderFailureClass
	}{
		{"plain 5h over file transient", transientFile, fiveHour, fiveHour},
		{"file 5h over plain transient", fiveHour, transientPlain, fiveHour},
		{"file transient keeps detail", transientFile, transientPlain, transientFile},
		{"plain transient over fatal", fatal, transientPlain, transientPlain},
		{"plain empty keeps file class", transientFile, empty, transientFile},
		{"both empty stays fatal default", fatal, empty, fatal},
	}
	for _, c := range cases {
		if got := mergePlainFailureClass(c.base, c.plain); got != c.want {
			t.Fatalf("%s: merge = %#v, want %#v", c.name, got, c.want)
		}
	}
}

func TestRateLimitStateSurvivesArtifactProtectionError(t *testing.T) {
	st := newStateStoreT(t)
	artifactDir, err := st.PrepareArtifactDir()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(artifactDir, "link")); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{
		output: zaiFiveHourLog,
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err = w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "p",
		Request: "req",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) || limitErr.ArtifactWarning == "" {
		t.Fatalf("artifact警告付きrate limit errorを期待: %v", err)
	}
	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || checkpoint.StopKind != state.ResumeStopRateLimited {
		t.Fatalf("rate-limit checkpointが保存されていません: checkpoint=%#v err=%v", checkpoint, loadErr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteResumeContinuesAfterRateLimit(t *testing.T) {
	st := newStateStoreT(t)
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
		ResetAtCST:     "2026-07-22 14:06:34",
		ResetAtRFC3339: "2026-07-22T14:06:34+08:00",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteResumeRestoresRateLimitedStatusAfterRunnerError(t *testing.T) {
	st := newStateStoreT(t)
	original := state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "req",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtCST:     "2026-07-22 14:06:34",
		ResetAtRFC3339: "2026-07-22T14:06:34+08:00",
	}
	if err := st.SaveResumeCheckpoint(original); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	r := &scriptedRunner{steps: []runnerStep{{
		output: "boom fatal session error\n",
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, r)
	err := w.ExecuteResume()
	var fatalErr *WorkerError
	if err == nil || !errors.As(err, &fatalErr) || !strings.Contains(fatalErr.Tail, "boom fatal session error") {
		t.Fatalf("runner errorを期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	restored, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || restored.StopKind != state.ResumeStopRateLimited {
		t.Fatalf("rate-limit checkpointが復元されていません: checkpoint=%#v err=%v", restored, loadErr)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("runner calls = %d", len(r.prompts))
	}
}

func seedWaitingDecision(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
}

func assertStoppedDecisionCheckpoint(t *testing.T, st *state.StateStore, status state.TaskStatus, stopKind state.ResumeStopKind) {
	t.Helper()
	if st.TaskStatus() != status || !st.Exists("pending-decision") {
		t.Fatalf("停止直後のstate: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	checkpoint, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || checkpoint.Stage != state.ResumeStageWorker || checkpoint.Phase != "worker-decision" ||
		checkpoint.StopKind != stopKind || checkpoint.Decision != "A案で進める" {
		t.Fatalf("decision停止checkpoint = %#v err=%v", checkpoint, cerr)
	}
	if got := st.ReadOr("last-decision", ""); got != "A案で進める" {
		t.Fatalf("last-decision = %q", got)
	}
	plan, perr := st.ParentActionPlan()
	if perr != nil || plan.RequiredAction != state.ParentActionResume || !plan.Allows(state.ParentActionResume) {
		t.Fatalf("decision停止のresume admission = %#v err=%v", plan, perr)
	}
}

func resumeStoppedDecisionResolvingPending(t *testing.T, st *state.StateStore, newWorkflow func(*scriptedRunner) *Workflow) {
	t.Helper()
	resumeRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("decision resumed")},
		{structured: needsSolReviewPacket()},
	}}
	if err := newWorkflow(resumeRunner).ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("resume後のstate: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if len(resumeRunner.phases) == 0 || resumeRunner.phases[0] != "worker-decision" {
		t.Fatalf("resume phases = %v", resumeRunner.phases)
	}
	if len(resumeRunner.prompts) == 0 || !strings.Contains(resumeRunner.prompts[0], "A案で進める") {
		t.Fatalf("resume promptがdecisionを保持していません: %#v", resumeRunner.prompts)
	}
}

func TestExecuteDecisionRateLimitResumeKeepsDecisionCheckpoint(t *testing.T) {
	st := newStateStoreT(t)
	seedWaitingDecision(t, st)
	if err := st.Write("worker.id", "decision-session"); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{
		output: zaiFiveHourLog,
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteDecision("A案で進める")
	var limitErr runner.ZaiRateLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	assertStoppedDecisionCheckpoint(t, st, state.TaskStatusRateLimited, state.ResumeStopRateLimited)
	if !st.Exists("worker.ready") || st.ReadOr("worker.id", "") != "decision-session" {
		t.Fatal("rate limit停止が同一sessionを保持していません")
	}
	resumeStoppedDecisionResolvingPending(t, st, func(r *scriptedRunner) *Workflow { return newWorkflowT(t, st, r) })
	if got := st.ReadOr("worker.id", ""); got != "decision-session" {
		t.Fatalf("resumeがworker sessionを保持していません: %q", got)
	}
}

func TestExecuteDecisionProviderUnavailableResumeKeepsDecisionCheckpoint(t *testing.T) {
	st := newStateStoreT(t)
	seedWaitingDecision(t, st)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteDecision("A案で進める")
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("provider unavailable errorを期待: %v", err)
	}
	assertStoppedDecisionCheckpoint(t, st, state.TaskStatusProviderUnavailable, state.ResumeStopProviderUnavailable)
	resumeStoppedDecisionResolvingPending(t, st, func(r *scriptedRunner) *Workflow { return newWorkflowT(t, st, r) })
}

func TestExecuteDecisionInterruptResumeKeepsDecisionCheckpoint(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingDecision(t, st)
	if err := st.Write("worker.id", "decision-session"); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "decision-session"},
		runErr: &runner.InterruptedCallError{Phase: "worker-decision"},
	}}}
	w := newGitWorkflowT(t, st, r, repo)
	stop := attachStop(t, w)
	r.onRun = func() { stop.Request() }

	err := w.ExecuteDecision("A案で進める")
	var stoppedErr *runner.InterruptedCallError
	if !errors.As(err, &stoppedErr) {
		t.Fatalf("interrupt errorを期待: %v", err)
	}
	assertStoppedDecisionCheckpoint(t, st, state.TaskStatusInterrupted, state.ResumeStopInterrupted)
	if !st.Exists("worker.ready") || st.ReadOr("worker.id", "") != "decision-session" {
		t.Fatal("interrupt停止が同一sessionを保持していません")
	}
	resumeStoppedDecisionResolvingPending(t, st, func(r *scriptedRunner) *Workflow { return newGitWorkflowT(t, st, r, repo) })
	if got := st.ReadOr("worker.id", ""); got != "decision-session" {
		t.Fatalf("resumeがworker sessionを保持していません: %q", got)
	}
}

func TestExecuteResumeContinuesReviewerStage(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageReview,
		Phase:          "reviewer-1",
		Role:           state.ReviewerRole,
		Model:          "sonnet",
		ReadOnly:       true,
		Effort:         "high",
		Prompt:         "review",
		OriginalPrompt: "review",
		Request:        "request",
		WorkerResult:   workerResultFromBody(`{"status":"IMPLEMENTED","risk":"LOW","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}`),
		ReviewNumber:   1,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if strings.Join(r.models, ",") != "sonnet" {
		t.Fatalf("resume model = %#v", r.models)
	}
}

func TestExecuteResumeContinuesAutoFixStage(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageAutoFix,
		Phase:          "worker-auto-fix-1",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "fix",
		OriginalPrompt: "fix",
		Request:        "request",
		ReviewNumber:   1,
		AutoFixes:      1,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteResumeRejectsUnknownStage(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStage("unknown"),
		Phase:          "unknown",
		Role:           state.WorkerRole,
		Model:          "opus",
		Prompt:         "prompt",
		OriginalPrompt: "prompt",
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteResume()
	if err == nil || !strings.Contains(err.Error(), "unknown resume stage") {
		t.Fatalf("unknown stage error = %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("不正stageでrunnerが呼ばれました: calls=%d", len(r.prompts))
	}
	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || checkpoint.StopKind != state.ResumeStopRateLimited || checkpoint.Stage != state.ResumeStage("unknown") {
		t.Fatalf("不正stageのcheckpointが保持されていません: checkpoint=%#v err=%v", checkpoint, loadErr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteNewTaskRejectsPendingAndRateLimitedTasks(t *testing.T) {
	t.Run("pending decision", func(t *testing.T) {
		st := newStateStoreT(t)
		if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			t.Fatal(err)
		}
		if err := st.Touch("pending-decision"); err != nil {
			t.Fatal(err)
		}
		w := newWorkflowT(t, st, &scriptedRunner{})
		err := w.ExecuteNewTask("replacement")
		if err == nil || !strings.Contains(err.Error(), "waiting for Sol decision") {
			t.Fatalf("pending error = %v", err)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		st := newStateStoreT(t)
		if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{Model: "opus", StopKind: state.ResumeStopRateLimited}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
			t.Fatal(err)
		}
		w := newWorkflowT(t, st, &scriptedRunner{})
		err := w.ExecuteNewTask("replacement")
		if err == nil || !strings.Contains(err.Error(), "rate-limited") {
			t.Fatalf("rate limit error = %v", err)
		}
	})
}
