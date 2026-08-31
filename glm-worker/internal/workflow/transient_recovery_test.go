package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type typedProbeRunner struct {
	scriptedRunner
}

var (
	errProbeTransient    = errors.New("API Error: 503 Service Unavailable")
	errProbeNonTransient = errors.New("401 Unauthorized: invalid api key")
	errProbeLocalExec    = errors.New("exec: 'claude': executable file not found in $PATH")
)

func newRecoveryWorkflowT(t *testing.T, st *state.StateStore, r *scriptedRunner) (*Workflow, *fakeClock) {
	t.Helper()
	w := newWorkflowT(t, st, r)
	clock := newFakeClock()
	w.now = clock.nowFunc
	w.sleep = clock.sleepFunc
	return w, clock
}

func workerCheckpoint() state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "p",
		Request: "req",
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func readRateLimitedFlag(st *state.StateStore) bool {
	cp, err := st.LoadResumeCheckpoint()
	return err == nil && cp.RateLimited
}

func TestRecoveryExhaustsToProviderUnavailable(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient},
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Classification != "http-503" || pErr.Probes != 4 {
		t.Fatalf("classification/probes = %q/%d", pErr.Classification, pErr.Probes)
	}
	if pErr.Elapsed > providerUnavailableDeadline {
		t.Fatalf("elapsed %sがdeadline %sを超過", pErr.Elapsed, providerUnavailableDeadline)
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.ProviderUnavailable || cp.ProviderUnavailableProbes != 4 ||
		cp.ProviderUnavailableClassification != "http-503" || cp.RateLimited {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if !st.Exists("worker.ready") {
		t.Fatal("同一session/checkpointを保持すべき: worker.readyが無い")
	}
	if len(r.probes) != 4 {
		t.Fatalf("probe回数 = %d", len(r.probes))
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule) {
		t.Fatalf("sleeps = %v want schedule %v", clock.sleeps, transientBackoffSchedule)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("runner Run呼出 = %d (initialだけ期待)", len(r.prompts))
	}
}

func TestRecoveryProbeSuccessThenResumeCompletes(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{structured: implementedPacket("recovered")},
		},
		probeErrs: []error{errProbeTransient, nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(workerCheckpoint())
	if err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	if result.Status != "IMPLEMENTED" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.probes) != 2 || len(r.prompts) != 2 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("回復成功時はresume checkpointがclearされるべき")
	}
}

func TestRecoveryFromPlainStdoutTransientSignal(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{
				runErr: errors.New("exit status 1"),
				result: runner.RunResult{PlainFailure: runner.ProviderFailureClass{
					Kind:   runner.ProviderFailureTransient,
					Detail: "http-503",
				}},
			},
			{structured: implementedPacket("recovered")},
		},
		probeErrs: []error{errProbeTransient, nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(workerCheckpoint())
	if err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	if result.Status != "IMPLEMENTED" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.probes) != 2 || len(r.prompts) != 2 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRecoveryLinksRetryToFailedCall(t *testing.T) {
	st := newStateStoreT(t)
	recoveredRuntime := &state.CallRuntime{
		WorkerRevision:           "recovered-revision",
		ClaudeBinResolved:        "/resolved/claude",
		InstructionSurfaceDigest: "instruction-digest",
	}
	r := &scriptedRunner{
		steps: []runnerStep{
			{
				runErr: errors.New("exit status 1"),
				result: runner.RunResult{PlainFailure: runner.ProviderFailureClass{
					Kind:   runner.ProviderFailureTransient,
					Detail: "http-503",
				}},
			},
			{structured: implementedPacket("recovered"), result: runner.RunResult{Runtime: recoveredRuntime}},
		},
		probeErrs: []error{errProbeTransient, nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	if _, err := w.runModel(workerCheckpoint()); err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	taskID, _ := st.TaskID()
	allLogs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	logs := make([]state.ModelCallLog, 0, 2)
	for _, entry := range allLogs {
		if entry.CallType == state.CallTypeTask {
			logs = append(logs, entry)
		}
	}
	if len(logs) != 2 {
		t.Fatalf("task logs = %#v", logs)
	}
	if logs[0].Outcome != "transient_error" || logs[0].RetryOf != "" {
		t.Fatalf("失敗call = %#v", logs[0])
	}
	if logs[1].Outcome != "success" || logs[1].RetryOf != logs[0].CallID {
		t.Fatalf("再実行callの因果 = outcome=%q retry_of=%q want %q", logs[1].Outcome, logs[1].RetryOf, logs[0].CallID)
	}
	if !strings.HasPrefix(logs[1].RetryReason, "transient-provider-failure:") {
		t.Fatalf("再試行理由 = %q", logs[1].RetryReason)
	}
	if logs[1].Runtime == nil || logs[1].Runtime.WorkerRevision != "recovered-revision" ||
		logs[1].Runtime.ClaudeBinResolved != "/resolved/claude" ||
		logs[1].Runtime.InstructionSurfaceDigest != "instruction-digest" {
		t.Fatalf("再実行callのruntime = %#v", logs[1].Runtime)
	}
}

func TestRecoveryAccountingSeparatesTaskAndProbeCalls(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{structured: implementedPacket("recovered")},
		},
		probeErrs: []error{errProbeTransient, nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	if _, err := w.runModel(workerCheckpoint()); err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	stats := currentStats(t, st)
	if stats.ModelCalls != 2 || stats.WorkerCalls != 2 || stats.ReviewerCalls != 0 {
		t.Fatalf("task call集計 = %#v", stats)
	}
	if stats.TransientRetries != 1 {
		t.Fatalf("transient retries = %d", stats.TransientRetries)
	}
	if stats.ProbeOutcome["probe_failure"] != 1 || stats.ProbeOutcome["probe_success"] != 1 {
		t.Fatalf("probe outcome = %+v", stats.ProbeOutcome)
	}
	probeCalls := stats.ProbeOutcome["probe_failure"] + stats.ProbeOutcome["probe_success"]
	if total := stats.ModelCalls + probeCalls; total != 4 {
		t.Fatalf("total AI calls = %d", total)
	}
	var taskRecords, probeRecords int
	var probeCost float64
	for _, l := range taskLogs(t, st) {
		switch l.CallType {
		case state.CallTypeTask:
			taskRecords++
		case state.CallTypeProbe:
			probeRecords++
			probeCost += l.TotalCostUSD
			if l.ClaudeAPIDurationMS != 100 || l.ResolvedModelUsage["glm-5.3"].CostUSD != 0.01 {
				t.Fatalf("probe telemetryのAPI duration/resolved model = %#v", l)
			}
		}
	}
	if taskRecords != 2 || probeRecords != 2 {
		t.Fatalf("telemetry records task=%d probe=%d", taskRecords, probeRecords)
	}
	if probeCost != 0.02 {
		t.Fatalf("probe cost = %v", probeCost)
	}
	if stats.InputTokensByAlias["opus"] != 0 || stats.OutputTokensByAlias["opus"] != 0 {
		t.Fatalf("probe tokenがtask集計へ混ざった: %#v", stats)
	}
}

func TestRecoveryResumeTransientRetriesNextBackoff(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{output: "API Error: 504 Gateway Timeout", runErr: errors.New("exit status 1")},
			{structured: implementedPacket("recovered")},
		},
		probeErrs: []error{nil, nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(workerCheckpoint())
	if err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	if result.Status != "IMPLEMENTED" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.probes) != 2 || len(r.prompts) != 3 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
}

func TestRecoveryStopsAtDeadline(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 529", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient},
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	bigStep := 90 * time.Minute
	w.sleep = func(d time.Duration) {
		clock.sleeps = append(clock.sleeps, d)
		clock.now = clock.now.Add(bigStep)
	}

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Probes < 1 || pErr.Probes > 4 {
		t.Fatalf("deadline到達時のprobe回数 = %d", pErr.Probes)
	}
	if pErr.Elapsed < providerUnavailableDeadline {
		t.Fatalf("elapsed %sがdeadline %sに満たない", pErr.Elapsed, providerUnavailableDeadline)
	}
	if pErr.Probes == 4 {
		t.Fatalf("deadline到達でprobe4回全消費は通常経路と区別不可: probes=%d", pErr.Probes)
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRecoveryResumeNonTransientFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{output: "401 Unauthorized: invalid api key", runErr: errors.New("exit status 1")},
		},
		probeErrs: []error{nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	if err == nil {
		t.Fatal("errorを期待")
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("probe成功後の非transient resume失敗はprovider-unavailableでなくWORKER_ERRORへ: %v", err)
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("非transient error時はresume checkpointがclearされるべき")
	}

	stats := currentStats(t, st)
	if stats.ModelCalls != 2 || stats.WorkerCalls != 2 || stats.TransientRetries != 1 {
		t.Fatalf("task call集計 = %#v", stats)
	}
	var taskRecords int
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			taskRecords++
		}
	}
	if taskRecords != stats.ModelCalls {
		t.Fatalf("task telemetry %d件がstats model_calls %dと不一致", taskRecords, stats.ModelCalls)
	}
}

func TestRecoveryResumeNonTransientFatalRecordsExecutedCall(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{
				output: "401 Unauthorized: invalid api key",
				runErr: errors.New("exit status 1"),
				result: runner.RunResult{
					SessionID:     "resumed-session",
					Resumed:       true,
					TopLevelUsage: runner.TokenUsage{InputTokens: 11, OutputTokens: 7},
					ModelUsage: map[string]runner.ModelUsage{
						"glm-5.3": {InputTokens: 11, OutputTokens: 7, CostUSD: 0.04},
					},
					DurationMS:   1234,
					TotalCostUSD: 0.04,
				},
			},
		},
		probeErrs: []error{nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("WORKER_ERRORを期待: %v", err)
	}
	if !strings.Contains(workerErr.Tail, "401 Unauthorized") {
		t.Fatalf("fatal応答本文がerror終端に無い: %q", workerErr.Tail)
	}
	if len(r.prompts) != 2 || len(r.probes) != 1 {
		t.Fatalf("runner Run呼出=%d probe=%d want 2/1", len(r.prompts), len(r.probes))
	}
	logs := taskLogs(t, st)
	var fatal state.ModelCallLog
	var transient, probeRecords int
	for _, l := range logs {
		switch {
		case l.CallType == state.CallTypeProbe:
			probeRecords++
		case l.CallType == state.CallTypeTask && l.Outcome == "transient_error":
			if l.ProviderClassification != "http-503" {
				t.Fatalf("初回transient記録のclassification = %q", l.ProviderClassification)
			}
			transient++
		case l.CallType == state.CallTypeTask && l.Outcome == "error":
			fatal = l
		}
	}
	if transient != 1 || probeRecords != 1 {
		t.Fatalf("task/probe記録 = transient:%d probe:%d want 1/1", transient, probeRecords)
	}
	if fatal.Phase != "worker-new" || fatal.SessionID != "resumed-session" || !fatal.Resumed {
		t.Fatalf("fatal記録の呼出識別 = phase:%q session:%q resumed:%v", fatal.Phase, fatal.SessionID, fatal.Resumed)
	}
	if fatal.TopLevelUsage.InputTokens != 11 || fatal.TopLevelUsage.OutputTokens != 7 {
		t.Fatalf("fatal記録のtoken = %+v", fatal.TopLevelUsage)
	}
	if usage := fatal.ResolvedModelUsage["glm-5.3"]; usage.CostUSD != 0.04 || usage.OutputTokens != 7 {
		t.Fatalf("fatal記録のresolved model = %+v", fatal.ResolvedModelUsage)
	}
	if fatal.TotalCostUSD != 0.04 || fatal.ClaudeDurationMS != 1234 {
		t.Fatalf("fatal記録のcost/duration = %v/%d", fatal.TotalCostUSD, fatal.ClaudeDurationMS)
	}
	if !strings.Contains(fatal.Response, "401 Unauthorized") || !strings.Contains(fatal.Error, "exit status 1") {
		t.Fatalf("fatal記録のresponse/error本文が失われた: response=%q error=%q", fatal.Response, fatal.Error)
	}
	stats := currentStats(t, st)
	if stats.InputTokensByAlias["opus"] != 11 || stats.OutputTokensByAlias["opus"] != 7 {
		t.Fatalf("fatal再開呼出のtokenがtask集計へ反映されていない: %#v", stats)
	}
	if stats.ProbeOutcome["probe_success"] != 1 {
		t.Fatalf("probe outcome = %+v", stats.ProbeOutcome)
	}
}

func TestRecoveryResumeFatalArtifactFailureKeepsSingleRecord(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{
				output: "401 Unauthorized: invalid api key",
				runErr: errors.New("exit status 1"),
				result: runner.RunResult{TopLevelUsage: runner.TokenUsage{InputTokens: 11, OutputTokens: 7}},
			},
		},
		probeErrs: []error{nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st.ArtifactDir(taskID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("outside-state", filepath.Join(st.ArtifactDir(taskID), "link")); err != nil {
		t.Fatal(err)
	}

	_, err = w.runModel(workerCheckpoint())
	if err == nil || !strings.Contains(err.Error(), "artifactの権限を保護できません") {
		t.Fatalf("artifact保護errorを期待: %v", err)
	}
	var taskRecords int
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			taskRecords++
		}
	}
	if taskRecords != 2 {
		t.Fatalf("task telemetry %d件 want 2(transient記録+fatal記録のみ)", taskRecords)
	}
	if stats := currentStats(t, st); stats.InputTokensByAlias["opus"] != 11 {
		t.Fatalf("state_error記録の追加でtokenが二重計上された: %#v", stats.InputTokensByAlias)
	}
}

func TestRecoveryDoesNotTriggerOnFiveHourLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{output: zaiFiveHourLog, runErr: errors.New("exit status 1")}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	if err == nil {
		t.Fatal("errorを期待")
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("5h上限はprovider-unavailableでなくRATE_LIMITEDへ: %v", err)
	}
	if len(r.probes) != 0 {
		t.Fatalf("5h上限でprobeが呼ばれた: %d", len(r.probes))
	}
	if !readRateLimitedFlag(st) {
		t.Fatal("5h上限でrate-limited checkpointが保存されるべき")
	}
}

func TestRecoveryDoesNotTriggerOnNonTransientError(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{output: "boom fatal", runErr: errors.New("exit status 1")}}}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	if err == nil {
		t.Fatal("errorを期待")
	}
	if len(r.probes) != 0 {
		t.Fatalf("非transient errorでprobeが呼ばれた: %d", len(r.probes))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("非transient error時はresume checkpointがclearされるべき")
	}
}

func TestProviderUnavailableTaskBlocksNewTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage: state.ResumeStageWorker, Phase: "worker-new", Role: state.WorkerRole,
		Model: "opus", Effort: "high", Prompt: "p", ProviderUnavailable: true,
		ProviderUnavailableClassification: "http-503", ProviderUnavailableProbes: 4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
	w := newWorkflowT(t, st, &scriptedRunner{})

	err := w.ExecuteNewTask("replacement")
	if err == nil || !strings.Contains(err.Error(), "provider-unavailable") {
		t.Fatalf("provider-unavailable taskの新規task開始を拒否すべき: %v", err)
	}
}

func TestResumeFromProviderUnavailableRetriesSameSession(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		Effort:                            "high",
		Prompt:                            "p",
		OriginalPrompt:                    "p",
		Request:                           "req",
		ProviderUnavailable:               true,
		ProviderUnavailableClassification: "http-503",
		ProviderUnavailableProbes:         4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatalf("resume成功を期待: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.probes) != 1 {
		t.Fatalf("probe成功後に本taskを1回resumeすべき: probes=%d", len(r.probes))
	}
	if len(r.prompts) != 2 {
		t.Fatalf("同一session/checkpointからworker→reviewerへ再試行すべき: prompts=%d", len(r.prompts))
	}
	if !strings.Contains(r.prompts[0], "RESUME_REASON: provider-unavailable") {
		t.Fatalf("provider-unavailable resume reason markerがpromptにない: %q", r.prompts[0])
	}
}

func TestResumeFromProviderUnavailableRestoresStatusAfterRunnerError(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		Effort:                            "high",
		Prompt:                            "p",
		OriginalPrompt:                    "p",
		Request:                           "req",
		ProviderUnavailable:               true,
		ProviderUnavailableClassification: "http-503",
		ProviderUnavailableProbes:         4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
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
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("resume失敗時はprovider-unavailable statusへ復元すべき: %q", st.TaskStatus())
	}
	restored, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || !restored.ProviderUnavailable {
		t.Fatalf("provider-unavailable checkpointが復元されていません: checkpoint=%#v err=%v", restored, loadErr)
	}
}

func seedProviderUnavailableCheckpoint(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		Effort:                            "high",
		Prompt:                            "p",
		OriginalPrompt:                    "p",
		Request:                           "req",
		ProviderUnavailable:               true,
		ProviderUnavailableClassification: "http-503",
		ProviderUnavailableProbes:         4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryProbeNonTransientFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeNonTransient},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	if err == nil {
		t.Fatal("errorを期待")
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("非transient probe errorはprovider-unavailableでなくWORKER_ERRORへ: %v", err)
	}
	if len(r.probes) != 1 {
		t.Fatalf("非transient probeで即fail closedすべき: probes=%d", len(r.probes))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("fail closed時はresume checkpointがclearされるべき")
	}

	stats := currentStats(t, st)
	if stats.ModelCalls != 1 || stats.TransientRetries != 0 {
		t.Fatalf("task call集計 = %#v", stats)
	}
	var taskRecords int
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			taskRecords++
		}
	}
	if taskRecords != stats.ModelCalls {
		t.Fatalf("task telemetry %d件がstats model_calls %dと不一致", taskRecords, stats.ModelCalls)
	}
}

func TestRecoveryProbeLocalExecErrorFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeLocalExec},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	if err == nil {
		t.Fatal("errorを期待")
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("local実行errorはprovider-unavailableでなくWORKER_ERRORへ: %v", err)
	}
	if len(r.probes) != 1 {
		t.Fatalf("local実行errorは即fail closedすべき: probes=%d", len(r.probes))
	}
}

func TestRecoveryAppliesJitter(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:     []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeErrs: []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient},
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	w.jitter = func(base time.Duration) time.Duration { return base + time.Minute }

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	want := []time.Duration{6 * time.Minute, 16 * time.Minute, 46 * time.Minute, 91 * time.Minute}
	if !equalDurations(clock.sleeps, want) {
		t.Fatalf("jitter適用後のsleeps = %v want %v", clock.sleeps, want)
	}
	if pErr.Elapsed > providerUnavailableDeadline {
		t.Fatalf("elapsed %sがdeadline %sを超過", pErr.Elapsed, providerUnavailableDeadline)
	}
}

func TestResumeProbeGateProviderStillDownZeroFullRuns(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps:     []runnerStep{{structured: implementedPacket("never used")}},
		probeErrs: []error{errProbeTransient, errProbeTransient, errProbeTransient, errProbeTransient},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteResume()
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("provider未回復時は本task Runが0回であるべき: %d", len(r.prompts))
	}
	if len(r.probes) != 4 {
		t.Fatalf("probe回数 = %d", len(r.probes))
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestResumeProbeGateFailClosedOnNonTransientProbe(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps:     []runnerStep{{structured: implementedPacket("never used")}},
		probeErrs: []error{errProbeNonTransient},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteResume()
	if err == nil {
		t.Fatal("errorを期待")
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("非transient probe errorはprovider-unavailableでなくfail closedへ: %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("本task Runが0回であるべき: %d", len(r.prompts))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("fail closed時はresume checkpointがclearされるべき")
	}
}

func TestRecoveryHitsFiveHourLimitSavesRateLimited(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
		},
		probeErrs: []error{nil},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"

	_, err := w.runModel(workerCheckpoint())
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("RATE_LIMITEDを期待: %v", err)
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("5h上限到達時はprovider-unavailableでない: %v", err)
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.RateLimited || cp.ProviderUnavailable {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.probes) != 1 || len(r.prompts) != 2 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
}

func TestRecoveryProbeBlankResponseRetriesToProviderUnavailable(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:              []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeBlankResponse: true,
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Probes != 4 {
		t.Fatalf("probe回数 = %d want 4", pErr.Probes)
	}
	if pErr.Classification != runner.ProbeContractFailure {
		t.Fatalf("classification = %q want %q", pErr.Classification, runner.ProbeContractFailure)
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule) {
		t.Fatalf("sleeps = %v want schedule %v", clock.sleeps, transientBackoffSchedule)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("本task resumeは1回(初回)だけのべき: %d", len(r.prompts))
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.ProviderUnavailable || cp.ProviderUnavailableClassification != runner.ProbeContractFailure ||
		cp.ProviderUnavailableProbes != 4 || cp.RateLimited {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if !st.Exists("worker.ready") {
		t.Fatal("停止時も同一session/checkpointを保持すべき: worker.readyが無い")
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRecoveryProbeContractFailureThenSuccessResumes(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{structured: implementedPacket("recovered")},
		},
		probeResponses: []string{""},
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(workerCheckpoint())
	if err != nil {
		t.Fatalf("回復成功を期待: %v", err)
	}
	if result.Status != "IMPLEMENTED" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.probes) != 2 || len(r.prompts) != 2 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule[:2]) {
		t.Fatalf("契約違反probe後のbackoff = %v want %v", clock.sleeps, transientBackoffSchedule[:2])
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("回復成功時はresume checkpointがclearされるべき")
	}
}

func TestRecoveryProbeContractFailureUntilDeadlineStopsUnavailable(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:              []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeBlankResponse: true,
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	w.sleep = func(d time.Duration) {
		clock.sleeps = append(clock.sleeps, d)
		clock.now = clock.now.Add(90 * time.Minute)
	}

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Probes < 1 || pErr.Probes > 3 {
		t.Fatalf("deadline到達時のprobe回数 = %d", pErr.Probes)
	}
	if pErr.Elapsed < providerUnavailableDeadline {
		t.Fatalf("elapsed %sがdeadline %sに満たない", pErr.Elapsed, providerUnavailableDeadline)
	}
	if pErr.Probes == 4 {
		t.Fatalf("deadline到達でprobe4回全消費はprobe上限経路と区別不可: probes=%d", pErr.Probes)
	}
	if pErr.Classification != runner.ProbeContractFailure {
		t.Fatalf("classification = %q want %q", pErr.Classification, runner.ProbeContractFailure)
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) != 1 {
		t.Fatalf("本task resumeは1回(初回)だけのべき: %d", len(r.prompts))
	}
}

func TestResumeProbeGateBlankResponseNotRecovered(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps:              []runnerStep{{structured: implementedPacket("never used")}},
		probeBlankResponse: true,
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteResume()
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Classification != runner.ProbeContractFailure {
		t.Fatalf("classification = %q want %q", pErr.Classification, runner.ProbeContractFailure)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("本task Runが0回であるべき: %d", len(r.prompts))
	}
	if len(r.probes) != 4 {
		t.Fatalf("probe回数 = %d want 4", len(r.probes))
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func (r *typedProbeRunner) Probe(model string) (runner.ProbeResult, error) {
	r.probes = append(r.probes, model)
	return runner.ProbeResult{}, &runner.ProbeInvalidResponseError{
		Model:  model,
		Reason: errors.New("応答本文が空です"),
	}
}

func TestRecoveryTypedInvalidResponseErrorRetriesToUnavailable(t *testing.T) {
	st := newStateStoreT(t)
	r := &typedProbeRunner{scriptedRunner{
		steps: []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
	}}
	w, clock := newRecoveryWorkflowT(t, st, &r.scriptedRunner)
	w.runner = r
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Probes != 4 || pErr.Classification != runner.ProbeContractFailure {
		t.Fatalf("typed不正応答もbackoff上限まで再試行すべき: probes=%d class=%q", pErr.Probes, pErr.Classification)
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule) {
		t.Fatalf("sleeps = %v want schedule %v", clock.sleeps, transientBackoffSchedule)
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr != nil || !st.Exists("worker.ready") {
		t.Fatalf("停止で保存taskが失われた: %v", cerr)
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestResumeFromProviderUnavailableHitsFiveHourLimit(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{steps: []runnerStep{
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"

	err := w.ExecuteResume()
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("RATE_LIMITEDを期待: %v", err)
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.RateLimited || cp.ProviderUnavailable {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRecoveryProbeFiveHourSignatureSavesRateLimited(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:          []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeResponses: []string{zaiFiveHourLog},
		probeIsError:   true,
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"

	_, err := w.runModel(workerCheckpoint())
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("RATE_LIMITEDを期待: %v", err)
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("5h signature probeはprovider-unavailableでない: %v", err)
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.RateLimited || cp.ProviderUnavailable {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if cp.ResetAtCST != "2026-07-22 14:06:34" || cp.ResetAtRFC3339 == "" {
		t.Fatalf("reset時刻 = %q/%q", cp.ResetAtCST, cp.ResetAtRFC3339)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.probes) != 1 || len(r.prompts) != 1 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
}

func TestResumeProbeGateFiveHourSignatureSavesRateLimited(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps:          []runnerStep{{structured: implementedPacket("never used")}},
		probeResponses: []string{zaiFiveHourLog},
		probeIsError:   true,
	}
	w := newWorkflowT(t, st, r)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"

	err := w.ExecuteResume()
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("RATE_LIMITEDを期待: %v", err)
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.RateLimited || cp.ProviderUnavailable || cp.ProviderUnavailableClassification != "" {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if st.TaskStatus() != state.TaskStatusRateLimited {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) != 0 || len(r.probes) != 1 {
		t.Fatalf("prompts=%d probes=%d", len(r.prompts), len(r.probes))
	}
}

func TestRecoveryProbeAuthSignalFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeResponses: []string{
			"401 Unauthorized: invalid api key",
			"API Error: 503 Service Unavailable",
		},
		probeIsError: true,
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("WORKER_ERRORを期待: %v", err)
	}
	var pErr *runner.ProviderUnavailableError
	var limitErr runner.ZaiRateLimitError
	if errors.As(err, &pErr) {
		t.Fatalf("auth信号はprovider-unavailableでない: %v", err)
	}
	if errors.As(err, &limitErr) {
		t.Fatalf("auth信号がrate-limitedへ誤分類: %v", err)
	}
	if len(r.probes) != 1 || len(r.prompts) != 1 {
		t.Fatalf("追加probeも本task再開もしない: probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("fail closed時はresume checkpointがclearされるべき")
	}
}

func TestResumeProbeGateAuthSignalFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps: []runnerStep{{structured: implementedPacket("never used")}},
		probeResponses: []string{
			"401 Unauthorized: invalid api key",
			"API Error: 503 Service Unavailable",
		},
		probeIsError: true,
	}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteResume()
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("WORKER_ERRORを期待: %v", err)
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("auth信号はprovider-unavailable再保存でない: %v", err)
	}
	if len(r.prompts) != 0 || len(r.probes) != 1 {
		t.Fatalf("本task resumeも追加probeもしない: prompts=%d probes=%d", len(r.prompts), len(r.probes))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("fail closed時はresume checkpointがclearされるべき")
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("fail closed時のstatus = %q", st.TaskStatus())
	}
}

func TestRecoveryProbeBareHTTPNumberStaysProbeContract(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeResponses: []string{
			"retry failed after waiting 400 ms",
			"retry failed after waiting 400 ms",
			"retry failed after waiting 400 ms",
			"retry failed after waiting 400 ms",
		},
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Classification != runner.ProbeContractFailure {
		t.Fatalf("classification = %q want %q", pErr.Classification, runner.ProbeContractFailure)
	}
	if len(r.probes) != 4 || len(r.prompts) != 1 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule) {
		t.Fatalf("backoff = %v want %v", clock.sleeps, transientBackoffSchedule)
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.ProviderUnavailable || cp.ProviderUnavailableClassification != runner.ProbeContractFailure {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
}

func TestRecoveryProbeMixedTransientAuthWordRetriesAsTransient(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{
			{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")},
			{structured: implementedPacket("recovered")},
		},
		probeResponses: []string{"API Error: 503 Service Unavailable · authentication failed"},
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(workerCheckpoint())
	if err != nil {
		t.Fatalf("混在信号はtransient retry後に回復する: %v", err)
	}
	if result.Status != "IMPLEMENTED" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.probes) != 2 || len(r.prompts) != 2 {
		t.Fatalf("probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("回復成功時はresume checkpointがclearされるべき")
	}
}

func TestRecoveryProbeBareAuthWordStaysProbeContract(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps: []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeResponses: []string{
			"This request is unauthorized for the current account",
			"Access to this model is forbidden during maintenance",
			"This request is unauthorized for the current account",
			"Access to this model is forbidden during maintenance",
		},
		probeIsError: true,
	}
	w, clock := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("裸語semantic invalidはWORKER_ERROR/fail-closedでなくprovider-unavailableへ: %v", err)
	}
	if pErr.Classification != runner.ProbeContractFailure || pErr.Probes != 4 {
		t.Fatalf("classification/probes = %q/%d", pErr.Classification, pErr.Probes)
	}
	if !equalDurations(clock.sleeps, transientBackoffSchedule) {
		t.Fatalf("backoff = %v want %v", clock.sleeps, transientBackoffSchedule)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("本task resumeは1回(初回)だけのべき: %d", len(r.prompts))
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.ProviderUnavailable || cp.ProviderUnavailableClassification != runner.ProbeContractFailure ||
		cp.ProviderUnavailableProbes != 4 || cp.RateLimited {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if !st.Exists("worker.ready") {
		t.Fatal("停止時も同一session/checkpointを保持すべき: worker.readyが無い")
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestResumeProbeGateBareAuthWordStaysUnavailable(t *testing.T) {
	st := newStateStoreT(t)
	seedProviderUnavailableCheckpoint(t, st)
	r := &scriptedRunner{
		steps: []runnerStep{{structured: implementedPacket("never used")}},
		probeResponses: []string{
			"This request is unauthorized for the current account",
			"Access to this model is forbidden during maintenance",
			"This request is unauthorized for the current account",
			"Access to this model is forbidden during maintenance",
		},
		probeIsError: true,
	}
	w, _ := newRecoveryWorkflowT(t, st, r)

	err := w.ExecuteResume()
	var pErr *runner.ProviderUnavailableError
	if !errors.As(err, &pErr) {
		t.Fatalf("ProviderUnavailableErrorを期待: %v", err)
	}
	if pErr.Classification != runner.ProbeContractFailure || pErr.Probes != 4 {
		t.Fatalf("classification/probes = %q/%d", pErr.Classification, pErr.Probes)
	}
	if len(r.prompts) != 0 || len(r.probes) != 4 {
		t.Fatalf("本task resumeも追加probe上限を超えない: prompts=%d probes=%d", len(r.prompts), len(r.probes))
	}
	cp, cerr := st.LoadResumeCheckpoint()
	if cerr != nil || !cp.ProviderUnavailable || cp.ProviderUnavailableClassification != runner.ProbeContractFailure ||
		cp.ProviderUnavailableProbes != 4 {
		t.Fatalf("checkpoint = %#v err=%v", cp, cerr)
	}
	if st.TaskStatus() != state.TaskStatusProviderUnavailable {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRecoveryProbeStatusAuthPhraseFailsClosed(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{
		steps:          []runnerStep{{output: "API Error: 503 Service Unavailable", runErr: errors.New("exit status 1")}},
		probeResponses: []string{"403 Forbidden"},
		probeIsError:   true,
	}
	w, _ := newRecoveryWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(workerCheckpoint())
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("WORKER_ERRORを期待: %v", err)
	}
	var pErr *runner.ProviderUnavailableError
	if errors.As(err, &pErr) {
		t.Fatalf("status組合せauth信号はprovider-unavailableでない: %v", err)
	}
	if len(r.probes) != 1 || len(r.prompts) != 1 {
		t.Fatalf("追加probeも本task再開もしない: probes=%d prompts=%d", len(r.probes), len(r.prompts))
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("fail closed時はresume checkpointがclearされるべき")
	}
}
