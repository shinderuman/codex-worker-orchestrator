package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runnerStep struct {
	output string
	runErr error
	result runner.RunResult

	structured string
}

type scriptedRunner struct {
	steps     []runnerStep
	probeErrs []error

	probeResponses     []string
	probeBlankResponse bool

	probeIsError bool

	onRun   func()
	onProbe func()
	prompts []string
	models  []string
	phases  []string

	readOnlyCalls []bool
	probes        []string

	artifactFiles   []scenarioArtifact
	taskArtifactDir func() (string, error)
}

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

const zaiFiveHourLog = "API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34]\n"

var fixedSnapshot = state.GitSnapshot{Head: "test-head", IndexDigest: "test-index", WorktreeDigest: "test-worktree"}

var testFixedTime = time.Unix(1_700_000_000, 0).UTC()

func (r *scriptedRunner) Run(
	_ state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	_ string,
	prompt string,
	outputPath string,
) (runner.RunResult, error) {
	r.prompts = append(r.prompts, prompt)
	r.models = append(r.models, model)
	r.phases = append(r.phases, phase)
	r.readOnlyCalls = append(r.readOnlyCalls, readOnly)
	index := len(r.prompts) - 1
	if r.onRun != nil {
		r.onRun()
	}
	step := r.steps[index]
	if r.taskArtifactDir != nil && (strings.Contains(step.structured, scenarioArtifactDirToken) || strings.Contains(step.output, scenarioArtifactDirToken)) {
		dir, err := r.taskArtifactDir()
		if err != nil {
			return runner.RunResult{}, err
		}
		for _, af := range r.artifactFiles {
			if err := os.WriteFile(filepath.Join(dir, af.Name), []byte(af.Content), 0o600); err != nil {
				return runner.RunResult{}, err
			}
		}
		step.structured = strings.ReplaceAll(step.structured, scenarioArtifactDirToken, dir)
		step.output = strings.ReplaceAll(step.output, scenarioArtifactDirToken, dir)
	}
	if step.output != "" {
		if err := os.WriteFile(outputPath, []byte(step.output), 0o600); err != nil {
			return runner.RunResult{}, err
		}
	}
	result := step.result
	if result.SessionID == "" {
		result.SessionID = "test-session"
	}
	if step.structured != "" {
		result.StructuredOutput = json.RawMessage(step.structured)
	}
	if result.Response == "" {

		result.Response = string(result.StructuredOutput)
	}
	return result, step.runErr
}

func (r *scriptedRunner) Probe(model string) (runner.ProbeResult, error) {
	r.probes = append(r.probes, model)
	index := len(r.probes) - 1
	if r.onProbe != nil {
		r.onProbe()
	}
	var err error
	if index < len(r.probeErrs) {
		err = r.probeErrs[index]
	}
	response := runner.ProbeSentinel
	if r.probeBlankResponse {
		response = ""
	} else if index < len(r.probeResponses) {
		response = r.probeResponses[index]
	}
	return runner.ProbeResult{
		Response:      response,
		IsError:       r.probeIsError,
		Usage:         runner.TokenUsage{InputTokens: 1, OutputTokens: 1},
		ModelUsage:    map[string]runner.ModelUsage{"glm-5.3": {InputTokens: 1, OutputTokens: 1, CostUSD: 0.01}},
		DurationMS:    50,
		DurationAPIMS: 100,
		TotalCostUSD:  0.01,
	}, err
}

func implementedPacket(summary string) string {
	return implementedPacketWithArtifacts(summary, "none")
}

func implementedPacketWithArtifacts(summary string, artifacts string) string {
	result := packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             summary,
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	}
	if artifacts != "none" && artifacts != "" {
		result.Artifacts = []string{artifacts}
	}
	return packetBody(result)
}

func implementedPacketWithRisk(summary string, risk string) string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.Risk(risk),
		Summary:             summary,
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func duplicatedImplementedPacket() string {
	return implementedPacket("first") + implementedPacket("second")
}

func passPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusPass,
		Risk:                packet.RiskLow,
		Summary:             "pass",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "none",
		ResidualRisk:        "none",
		Targets:             []string{"final diff"},
	})
}

func needsSolReviewPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             "review",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "i",
		ResidualRisk:        "r",
		Targets:             []string{"t"},
		SolQuestion:         "q",
	})
}

func needsSolDecisionPacket() string {
	return packetBody(packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "tests",
		Targets:         []string{"t"},
	})
}

func fixRequiredPacket() string {
	return fixRequiredPacketWithTargets("t")
}

func fixRequiredPacketWithTargets(targets string) string {
	return packetBody(packet.Result{
		Status:              packet.StatusFixRequired,
		Risk:                packet.RiskHigh,
		Summary:             "fix",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "i",
		ResidualRisk:        "r",
		Targets:             []string{targets},
	})
}

func unknownStatusPacket() string {
	return packetBody(packet.Result{Status: packet.Status("UNKNOWN"), Risk: packet.RiskLow, Summary: "x"})
}

func seedReviewStartSnapshot(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.SaveReviewStartSnapshot(fixedSnapshot); err != nil {
		t.Fatal(err)
	}
}

func newStateStoreT(t *testing.T) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "testhash",
		RepoRoot:  "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(qualitySurfaceBaselineStateKey, "quality-baseline"); err != nil {
		t.Fatal(err)
	}
	return st
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: testFixedTime}
}

func (c *fakeClock) nowFunc() time.Time { return c.now }

func (c *fakeClock) sleepFunc(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func newWorkflowT(t *testing.T, st *state.StateStore, r *scriptedRunner) *Workflow {
	t.Helper()
	return newWorkflowTWithOutput(t, st, r, io.Discard)
}

func newWorkflowTWithOutput(t *testing.T, st *state.StateStore, r *scriptedRunner, output io.Writer) *Workflow {
	t.Helper()
	w := NewWorkflow(config.AppConfig{
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		TelemetryContent:      true,
		RepoRoot:              t.TempDir(),
	}, st, r, output)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		return fixedSnapshot, nil
	}
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
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, nil
	}
	clock := newFakeClock()
	w.now = clock.nowFunc
	w.sleep = clock.sleepFunc
	w.jitter = identityJitter
	w.qualityGate = func(string) (harnesslint.Report, error) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	w.captureQualitySurface = func(string) (string, error) { return "quality-baseline", nil }
	return w
}

func TestQualityGateBlocksReviewerAndRoutesWorkerFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	calls := 0
	w.qualityGate = func(string) (harnesslint.Report, error) {
		calls++
		if calls == 1 {
			return harnesslint.Report{Status: "fail", Violations: []harnesslint.Violation{{Rule: "funlen", Path: "a.go", Line: 3, Column: 1, Message: "too long"}}}, nil
		}
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.phases, []string{"worker-new", "worker-auto-fix-1", "reviewer-2-high-floor"}) {
		t.Fatalf("phases = %v", r.phases)
	}
	if calls != 2 {
		t.Fatalf("harnesslint calls = %d", calls)
	}
	if !strings.Contains(r.prompts[1], "harnesslint") {
		t.Fatalf("fix prompt = %s", r.prompts[1])
	}
	status := st.TaskStatus()
	if status != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %s", status)
	}
}

func identityJitter(base time.Duration) time.Duration { return base }

func TestRunModelRecordsPromptResponseAndUsage(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		structured: implementedPacket("done"),
		result: runner.RunResult{
			SessionID: "worker-session",
			TopLevelUsage: runner.TokenUsage{
				InputTokens:          1,
				CacheReadInputTokens: 2,
				OutputTokens:         3,
			},
			ModelUsage: map[string]runner.ModelUsage{
				"glm-5.3": {InputTokens: 10, CacheCreationInputTokens: 20, CacheReadInputTokens: 30, OutputTokens: 40},
				"glm-4.7": {InputTokens: 5, CacheReadInputTokens: 7, OutputTokens: 8},
			},
			DurationMS:    1200,
			DurationAPIMS: 900,
			TopLevelTurns: 2,
			SystemPrompt:  "worker system instruction",
		},
	}}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageWorker,
		Phase:  "worker-new",
		Role:   state.WorkerRole,
		Model:  "opus",
		Effort: "high",
		Prompt: "implementation instruction",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("telemetry logs = %#v", logs)
	}
	got := logs[0]
	wantResponse := implementedPacket("done")
	if !strings.HasPrefix(got.Prompt, "implementation instruction\n\nREPORT_ARTIFACT_DIR: ") || got.SystemPrompt != "worker system instruction" || got.Response != wantResponse {
		t.Fatalf("telemetry content = %#v", got)
	}
	if !strings.Contains(r.prompts[0], artifactPromptMarker) {
		t.Fatalf("artifact保存先がrunner promptにありません: %q", r.prompts[0])
	}
	if len(r.phases) != 1 || r.phases[0] != "worker-new" {
		t.Fatalf("runnerへ渡したphase = %#v", r.phases)
	}
	if got.TopLevelUsage.CacheReadInputTokens != 2 || got.TreeUsage.CacheReadInputTokens != 37 || got.ResolvedModelUsage["glm-5.3"].OutputTokens != 40 {
		t.Fatalf("telemetry usage = %#v", got)
	}
	stats := currentStats(t, st)
	if stats.CacheReadInputTokensByAlias["opus"] != 37 || stats.OutputTokensByAlias["opus"] != 48 || stats.OutputTokensByResolvedModel["glm-5.3"] != 40 {
		t.Fatalf("token stats = %#v", stats)
	}
}

func TestRunModelCanOmitTelemetryContent(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w := newWorkflowT(t, st, r)
	w.config.TelemetryContent = false
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageWorker,
		Phase:  "worker-new",
		Role:   state.WorkerRole,
		Model:  "opus",
		Effort: "high",
		Prompt: "secret instruction",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := st.TaskID()
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if logs[0].Prompt != "" || logs[0].SystemPrompt != "" || logs[0].Response != "" || logs[0].PromptSHA256 == "" || logs[0].ResponseSHA256 == "" {
		t.Fatalf("content無効時のtelemetry = %#v", logs[0])
	}
}

func currentStats(t *testing.T, st *state.StateStore) state.TaskStats {
	t.Helper()
	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.TaskID == taskID {
			return s
		}
	}
	t.Fatalf("current task stats not found: taskID=%s", taskID)
	return state.TaskStats{}
}

func packetBody(result packet.Result) string {
	data, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func resultFromBody(body string) packet.Result {
	value, err := packet.ParseStructured([]byte(body))
	if err != nil {
		panic(err)
	}
	return value
}

func workerResultFromBody(body string) *packet.Result {
	value := resultFromBody(body)
	return &value
}

func workerPacketWithRisk(risk string) string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.Risk(risk),
		Summary:             "done",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func constraintViolatingImplementedPacket() string {
	return packetBody(packet.Result{
		Status:     packet.StatusImplemented,
		Risk:       packet.RiskLow,
		Summary:    "done",
		Tests:      "pass",
		Unverified: "none",
	})
}

func oversizeImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             strings.Repeat("x", packet.MaxFieldBytes+1),
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func TestBoundedTextKeepsSingleLineFieldContract(t *testing.T) {
	omissionMarker := "[前方を省略] "
	withinLimit := "そのまま返す観測値"
	if got := boundedText(withinLimit, packet.MaxFieldBytes); got != withinLimit {
		t.Fatalf("上限内の値は変更しない: %q", got)
	}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"ascii超過", strings.Repeat("x", packet.MaxFieldBytes+40)},
		{"multibyte超過", strings.Repeat("あ", packet.MaxFieldBytes/3+13)},
		{"切詰め境界がrune途中のmultibyte超過", "x" + strings.Repeat("あ", packet.MaxFieldBytes/3+13)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundedText(tc.value, packet.MaxFieldBytes)
			if len(got) > packet.MaxFieldBytes {
				t.Fatalf("出力がbyte上限を超えています: %d bytes", len(got))
			}
			if !strings.HasPrefix(got, omissionMarker) {
				t.Fatalf("切詰め時に省略markerを先頭に置く: %q", got)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Fatalf("切詰め出力に改行を含めている: %q", got)
			}
			if !utf8.ValidString(got) {
				t.Fatal("切詰め出力が不正UTF-8です")
			}
			if !strings.HasSuffix(tc.value, got[len(omissionMarker):]) {
				t.Fatal("切詰め出力は元の値の末尾を保持すべきです")
			}
		})
	}
}

func TestRunModelCorrectsInvalidResultInSameRunner(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: oversizeImplementedPacket()},
		{structured: implementedPacket("implemented")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusImplemented {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.prompts) != 2 || !strings.Contains(r.prompts[1], "意味検証に不合格") {
		t.Fatalf("same runnerで修正再依頼されていません: %#v", r.prompts)
	}
	if r.phases[1] != "worker-new"+resultCorrectionPhaseSuffix {
		t.Fatalf("修正再依頼phase = %q", r.phases[1])
	}

	stats := currentStats(t, st)
	if stats.ModelCalls != 2 || stats.ResultCorrections != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "size" || logs[1].Outcome != "success" {
		t.Fatalf("result correction telemetry = %#v", logs)
	}
	if logs[0].RetryOf != "" || logs[0].RetryReason != "" {
		t.Fatalf("最初の失敗callに再試行因果があってはいません: %#v", logs[0])
	}
	if logs[1].RetryOf != logs[0].CallID || logs[1].RetryReason != "invalid-packet-result-correction" {
		t.Fatalf("修正再実行の因果 = retry_of=%q reason=%q want call %q", logs[1].RetryOf, logs[1].RetryReason, logs[0].CallID)
	}
}

func TestRunModelFailsClosedOnStructuredMismatch(t *testing.T) {
	tests := []struct {
		name     string
		role     state.SessionRole
		stage    state.ResumeStage
		readOnly bool
		phase    string
	}{
		{name: "worker", role: state.WorkerRole, stage: state.ResumeStageWorker, phase: "worker-new"},
		{name: "reviewer", role: state.ReviewerRole, stage: state.ResumeStageReview, readOnly: true, phase: "reviewer-1"},
	}
	var workerErr *WorkerError
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{
				{structured: duplicatedImplementedPacket()},
				{structured: implementedPacket("unreachable")},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err := w.runModel(state.ResumeCheckpoint{
				Stage:    test.stage,
				Phase:    test.phase,
				Role:     test.role,
				ReadOnly: test.readOnly,
				Model:    "opus",
				Effort:   "high",
				Prompt:   "original",
				Request:  "request",
			})
			if err == nil || !errors.As(err, &workerErr) {
				t.Fatalf("mismatch fail closedを期待: %v", err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("mismatchで修正再依頼は実行しない: calls=%d", len(r.prompts))
			}
			taskID, _ := st.TaskID()
			logs, logErr := st.ReadModelCallLogs(taskID)
			if logErr != nil {
				t.Fatal(logErr)
			}
			if len(logs) != 1 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "schema-mismatch" {
				t.Fatalf("structured mismatch telemetry = %#v", logs)
			}
			if !strings.Contains(logs[0].Error, "structured_output") {
				t.Fatalf("拒否理由がtelemetryへ記録されていません: %q", logs[0].Error)
			}
			stats := currentStats(t, st)
			if stats.ResultCorrections != 0 || stats.PacketRejectByCategory["schema-mismatch"] != 1 {
				t.Fatalf("stats = %#v", stats)
			}
		})
	}
}

func TestRunModelFailsClosedOnStructuredOutputError(t *testing.T) {
	cases := []struct {
		name             string
		runErr           error
		want             string
		wantRetryMetrics int
	}{
		{"retry exhausted", &runner.StructuredOutputError{Subtype: "error_max_structured_output_retries", TerminalReason: "gave up after 3 attempts"}, "error_max_structured_output_retries", 1},
		{"missing on success", &runner.StructuredOutputError{}, "result eventにstructured_outputがありません", 0},
	}
	var workerErr *WorkerError
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{
				{output: "", runErr: c.runErr},
				{structured: implementedPacket("unreachable")},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()
			w.config.RepoRoot = "/repo"
			w.config.RepoShort = "testrepo1234"

			_, err := w.runModel(state.ResumeCheckpoint{
				Stage:   state.ResumeStageWorker,
				Phase:   "worker-new",
				Role:    state.WorkerRole,
				Model:   "opus",
				Effort:  "high",
				Prompt:  "p",
				Request: "req",
			})
			if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("structured output fail closedを期待: %v", err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("transient retryにも修正再依頼にも入らない: calls=%d", len(r.prompts))
			}
			taskID, _ := st.TaskID()
			logs, logErr := st.ReadModelCallLogs(taskID)
			if logErr != nil {
				t.Fatal(logErr)
			}
			if len(logs) != 1 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "structured-output" {
				t.Fatalf("structured output telemetry = %#v", logs)
			}
			if _, cpErr := st.LoadResumeCheckpoint(); cpErr == nil {
				t.Fatal("resume checkpointが残っています")
			}
			stats := currentStats(t, st)

			if stats.StructuredRetryExhausted != c.wantRetryMetrics || stats.ResultCorrections != 0 {
				t.Fatalf("stats = %#v", stats)
			}
		})
	}
}

func TestExecuteEmitsAcceptedResultExactlyOnce(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacketWithRisk("done", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	buf := &bytes.Buffer{}
	w.output = buf

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), `"status":"`); got != 1 {
		t.Fatalf("受理結果の出力回数 = %d\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), `"status":"PASS"`) || strings.Contains(buf.String(), `"summary":"done"`) {
		t.Fatalf("旧応答が最終stdoutへ混入しています: %s", buf.String())
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) != 4 {
		t.Fatalf("修正再依頼・再出力は各1回だけ: calls=%d", len(r.prompts))
	}
	if strings.Join(r.models, ",") != "opus,opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestEmitResultRecordsEmittedPayloadBytes(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	var out bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &out)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	emitted := strings.TrimRight(out.String(), "\n")
	if !strings.Contains(emitted, `"status":"PASS"`) {
		t.Fatalf("受理結果がstdoutへ出ていません: %s", out.String())
	}
	if stats := currentStats(t, st); stats.SolPacketBytes != len(emitted) {
		t.Fatalf("SolPacketBytes = %d want %d(stdout payload bytes):\n%s", stats.SolPacketBytes, len(emitted), out.String())
	}
}

func TestRunModelStopsAfterRepeatedConstraintViolations(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "必須field requirement_coverage") {
		t.Fatalf("修正再依頼後の不合格停止を期待: %v", err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("修正再依頼は1回だけ実施する: calls=%d", len(r.prompts))
	}
}

func TestRunModelPreservesResultCorrectionAcrossRateLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original implementation prompt",
		OriginalPrompt: "original implementation prompt",
		Request:        "request",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("修正再依頼中のrate limit errorを期待: %v", err)
	}

	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !checkpoint.ResultCorrection || !strings.Contains(checkpoint.OriginalPrompt, "意味検証に不合格") {
		t.Fatalf("修正再依頼promptがcheckpointに保持されていません: %#v", checkpoint)
	}
}

func TestRunModelCorrectsArtifactOutsideTaskDir(t *testing.T) {
	st := newStateStoreT(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithArtifacts("invalid artifact", outside)},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || len(r.prompts) != 2 {
		t.Fatalf("artifact pathが修正されていません: result=%#v prompts=%d", result, len(r.prompts))
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "artifacts" || !strings.Contains(logs[0].Error, "artifact dir配下") {
		t.Fatalf("artifact validation telemetry = %#v", logs)
	}
}

func TestRunModelPreservesResultCorrectionPromptAcrossRateLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: oversizeImplementedPacket()},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original implementation prompt",
		OriginalPrompt: "original implementation prompt",
		Request:        "request",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("修正再依頼中のrate limit errorを期待: %v", err)
	}

	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !checkpoint.ResultCorrection || !strings.Contains(checkpoint.OriginalPrompt, "意味検証に不合格") {
		t.Fatalf("修正再依頼promptがcheckpointに保持されていません: %#v", checkpoint)
	}
	resumed := resumePrompt(checkpoint)
	if !strings.Contains(resumed, "意味検証に不合格") || strings.Contains(resumed, "original implementation prompt") {
		t.Fatalf("resumeが修正再依頼工程を指していません: %s", resumed)
	}
}

func TestExecuteExplicitFixRejectsCompletedTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}

	w := newWorkflowT(t, st, &scriptedRunner{})
	err := w.ExecuteExplicitFix("fix", "", "")
	if err == nil || !strings.Contains(err.Error(), "only available after NEEDS_SOL_REVIEW") {
		t.Fatalf("completed taskの--fixを拒否する必要があります: %v", err)
	}
}

func TestResumePromptUsesOriginalPrompt(t *testing.T) {
	checkpoint := state.ResumeCheckpoint{
		Prompt:         "already wrapped resume prompt",
		OriginalPrompt: "ORIGINAL TASK",
	}

	prompt := resumePrompt(checkpoint)
	if !strings.Contains(prompt, "ORIGINAL TASK") {
		t.Fatalf("original prompt missing: %s", prompt)
	}
	if strings.Contains(prompt, "already wrapped resume prompt") {
		t.Fatalf("resume prompt nested previous resume prompt: %s", prompt)
	}
}

func TestExecuteNewTaskReachesPass(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if strings.Join(r.models, ",") != "opus,haiku" {
		t.Fatalf("models = %#v", r.models)
	}
	if !strings.Contains(r.prompts[0], artifactPromptMarker) {
		t.Fatalf("worker promptにartifact保存先がありません: %q", r.prompts[0])
	}
	if strings.Contains(r.prompts[1], artifactPromptMarker) {
		t.Fatalf("read-only reviewerへartifact書込指示を渡しています: %q", r.prompts[1])
	}
}

func TestHighRiskWorkerUsesHighRiskReviewer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("done", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestExecuteNewTaskNeedsSolDecision(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if !st.Exists("pending-decision") {
		t.Fatal("pending-decisionが設定されていません")
	}
}

func TestExecuteNewTaskNeedsSolReview(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestExecuteDecisionContinuesPendingTask(t *testing.T) {
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
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("decision applied")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteDecision("A案で進める"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("decision後のstate: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if decision := st.ReadOr("last-decision", ""); decision != "A案で進める" {
		t.Fatalf("last-decision = %q", decision)
	}
	if len(r.prompts) == 0 || !strings.Contains(r.prompts[0], "A案で進める") {
		t.Fatalf("decision prompt = %#v", r.prompts)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestExecuteExplicitFixContinuesSolReviewTask(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "review"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("explicit fix")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteExplicitFix("境界値を修正する", "", ""); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) == 0 || !strings.Contains(r.prompts[0], "境界値を修正する") {
		t.Fatalf("fix prompt = %#v", r.prompts)
	}
	if stats := currentStats(t, st); stats.FixCommands != 1 {
		t.Fatalf("fix stats = %#v", stats)
	}
	if strings.Join(r.models, ",") != "opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestAutoFixNonConvergence(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fix")},
		{structured: fixRequiredPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.config.MaxAutoFixRounds = 1

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if strings.Join(r.models, ",") != "opus,haiku,opus,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestAutoFixCanRequestSolDecision(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("auto-fix decision state: status=%q pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestAutoFixRejectsReviewerStatus(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "worker結果のstatus") {
		t.Fatalf("auto-fix role status error = %v", err)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("修正再依頼はしない: calls=%d", len(r.prompts))
	}
}

func TestWorkerRejectsReviewerStatus(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	if err == nil || !strings.Contains(err.Error(), "worker結果のstatusとして許容されません") {
		t.Fatalf("worker role status error = %v", err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("schema保証範囲のため修正再依頼はしない: calls=%d", len(r.prompts))
	}
}

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

func TestRunModelSurfacesWorkerError(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{runErr: errors.New("boom")}}}
	w := newWorkflowT(t, st, r)
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
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("worker errorを期待: %v", err)
	}
	if _, cerr := st.LoadResumeCheckpoint(); cerr == nil {
		t.Fatal("resume checkpointはクリアされる必要があります")
	}
}

func TestRunModelRejectsMissingModelBeforeRunnerCall(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageWorker,
		Phase:  "worker-new",
		Role:   state.WorkerRole,
		Prompt: "p",
	})
	if err == nil || !strings.Contains(err.Error(), "checkpoint model is missing") {
		t.Fatalf("missing model error = %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("model未指定でrunnerが呼ばれました: calls=%d", len(r.prompts))
	}
}

func TestReviewerFormatError(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	if err == nil || !strings.Contains(err.Error(), "without an active task decision boundary") {
		t.Fatalf("reviewer role status error = %v", err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("schema保証範囲のため修正再依頼はしない: calls=%d", len(r.prompts))
	}
}

func TestReviewerUnknownStatusFailsClosedWithoutCorrection(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: unknownStatusPacket()},
	}}
	w := newWorkflowT(t, st, r)

	err := w.ExecuteNewTask("request")
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("未知STATUSのfail closed停止を期待: %v", err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("schema保証範囲のため修正再依頼はしない: calls=%d", len(r.prompts))
	}
}

func TestReviewNeedsHighRiskFloor(t *testing.T) {
	lowWorker := resultFromBody(`{"status":"IMPLEMENTED","risk":"LOW"}`)
	highWorker := resultFromBody(`{"status":"IMPLEMENTED","risk":"HIGH"}`)

	tests := []struct {
		name           string
		workerResult   packet.Result
		autoFixes      int
		hasDecision    bool
		hasPriorReview bool
		want           bool
	}{
		{"low worker fresh", lowWorker, 0, false, false, false},
		{"high worker", highWorker, 0, false, false, true},
		{"after autofix", lowWorker, 1, false, false, true},
		{"after decision", lowWorker, 0, true, false, true},
		{"after prior review", lowWorker, 0, false, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewNeedsHighRiskFloor(tt.workerResult, tt.autoFixes, tt.hasDecision, tt.hasPriorReview); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestRiskFloorFailClosedPacketIsValid(t *testing.T) {
	passPkt := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"reviewer pass","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)

	enforced := riskFloorFailClosedResult(passPkt)
	if enforced.Status != packet.StatusNeedsSolReview || enforced.Risk != packet.RiskHigh {
		t.Fatalf("status=%s risk=%s", enforced.Status, enforced.Risk)
	}
	if err := validateTypedResult(enforced); err != nil {
		t.Fatalf("fail closed結果がvalidate不合格: %v", err)
	}
	if enforced.RequirementCoverage == "covered" {
		t.Fatalf("reviewerのPASS内容をfail closed結果へ捏造している: %#v", enforced)
	}
}

func TestResolveRiskFloorReemitAcceptsCompliantAndFailsClosed(t *testing.T) {
	compliant := resultFromBody(`{"status":"NEEDS_SOL_REVIEW","risk":"HIGH","summary":"reviewer reemit","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["t"],"sol_question":"q"}`)
	if resolved := resolveRiskFloorReemit(compliant); resolved.Status != packet.StatusNeedsSolReview {
		t.Fatalf("準拠再出力はそのまま採用すべき: %#v", resolved)
	}

	passed := resultFromBody(`{"status":"PASS","risk":"LOW","summary":"pass again","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"none","residual_risk":"none","targets":["none"]}`)
	closed := resolveRiskFloorReemit(passed)
	if closed.Status != packet.StatusNeedsSolReview || !strings.Contains(closed.Summary, "PASS") {
		t.Fatalf("再違反はfail closedのNEEDS_SOL_REVIEWへ昇格すべき: %#v", closed)
	}
}

func TestRiskFloorReemitPromptConstraints(t *testing.T) {
	prompt := riskFloorReemitPrompt()
	for _, want := range []string{
		"NEEDS_SOL_REVIEW (RISK: HIGH) だけ",
		"実装・調査・テストをやり直さず",
		"結果だけを再出力",
		"TARGETSにはnoneを指定できません",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("reemit promptに%qがありません: %s", want, prompt)
		}
	}
}

func TestFixRequiredTargetsPacketDispatchesReportOnlyPrompt(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixRequiredPacketWithTargets("PACKET")},
		{structured: implementedPacketWithRisk("report re-emitted", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if got, want := strings.Join(r.models, ","), "opus,sonnet,opus,sonnet"; got != want {
		t.Fatalf("model routing = %q want %q", got, want)
	}
	if len(r.prompts) != 4 {
		t.Fatalf("prompt count = %d want 4", len(r.prompts))
	}
	reportOnly := r.prompts[2]
	for _, want := range []string{
		"報告だけを再出力",
		"実装・working tree変更・追加調査・test/lint/build/self-reviewをやり直さず",
	} {
		if !strings.Contains(reportOnly, want) {
			t.Fatalf("report-only promptに%qがありません: %s", want, reportOnly)
		}
	}
	for _, forbidden := range []string{
		"独立reviewerの指摘を修正してください",
		"修正後に必要なテスト・lint・build・自己レビューまで行ってください",
	} {
		if strings.Contains(reportOnly, forbidden) {
			t.Fatalf("report-only promptにimplementation fix文言%qが入っています: %s", forbidden, reportOnly)
		}
	}
	var reportOnlyPhases []string
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			reportOnlyPhases = append(reportOnlyPhases, l.Phase)
		}
	}
	if !slices.Contains(reportOnlyPhases, "worker-report-only-1") {
		t.Fatalf("telemetryへreport-only phaseが識別できません: %v", reportOnlyPhases)
	}
}

func TestFixRequiredOtherTargetsKeepsImplementationAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	implementation := r.prompts[2]
	for _, want := range []string{
		"独立reviewerの指摘を修正してください",
		"修正後に必要なテスト・lint・build・自己レビューまで行ってください",
	} {
		if !strings.Contains(implementation, want) {
			t.Fatalf("implementation auto-fix promptから%qが失われています: %s", want, implementation)
		}
	}
	if strings.Contains(implementation, "報告だけを再出力") {
		t.Fatalf("通常FIX_REQUIREDへreport-only promptが使われています: %s", implementation)
	}
	var phases []string
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			phases = append(phases, l.Phase)
		}
	}
	if !slices.Contains(phases, "worker-auto-fix-1") {
		t.Fatalf("telemetryへimplementation auto-fix phaseがありません: %v", phases)
	}
}

func TestFixRequiredWithoutTargetsCorrectsBeforeAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	fixWithoutTargets := `{"status":"FIX_REQUIRED","risk":"HIGH","summary":"fix","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":[],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixWithoutTargets},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix, "worker-auto-fix-1", "reviewer-2-high-floor"}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[2])
	}
	autoFix := r.prompts[3]
	if !strings.Contains(autoFix, `"targets":["glm-worker/internal/state/store.go:Read"]`) {
		t.Fatalf("auto-fix promptへ修正対象targetsが伝わっていません: %s", autoFix)
	}
	for i, prompt := range r.prompts {
		if i != 3 && strings.Contains(prompt, "MODE: APPLY_REVIEW_FIX") {
			t.Fatalf("targets未確定のFIX_REQUIREDがauto-fixへdispatchしています: prompt %d", i)
		}
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 5 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	stats := currentStats(t, st)
	if stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}

func TestNeedsSolDecisionWithoutTargetsCorrectsBeforeParentDispatch(t *testing.T) {
	st := newStateStoreT(t)
	decisionWithoutTargets := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":[],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: decisionWithoutTargets},
		{structured: needsSolDecisionPacket()},
	}}
	var emitted bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &emitted)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-new" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[1], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[1])
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("decision待ち状態へ遷移していません: status=%q pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if !strings.Contains(emitted.String(), `"status":"NEEDS_SOL_DECISION"`) || !strings.Contains(emitted.String(), `"targets":["t"]`) {
		t.Fatalf("親境界の結果packetへ修正後targetsが伝わっていません: %s", emitted.String())
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}

func TestFixRequiredBlankTargetsElementCorrectsBeforeAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	fixWithBlankElement := `{"status":"FIX_REQUIRED","risk":"HIGH","summary":"fix","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["   "],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixWithBlankElement},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix, "worker-auto-fix-1", "reviewer-2-high-floor"}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") || !strings.Contains(r.prompts[2], "TARGETSの要素は空・空白のみ") {
		t.Fatalf("修正再依頼promptへ要素違反が伝わっていません: %s", r.prompts[2])
	}
	autoFix := r.prompts[3]
	if !strings.Contains(autoFix, `"targets":["glm-worker/internal/state/store.go:Read"]`) {
		t.Fatalf("auto-fix promptへ修正対象targetsが伝わっていません: %s", autoFix)
	}
	for i, prompt := range r.prompts {
		if i != 3 && strings.Contains(prompt, "MODE: APPLY_REVIEW_FIX") {
			t.Fatalf("要素違反FIX_REQUIREDがauto-fixへdispatchしています: prompt %d", i)
		}
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 5 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
}

func TestNeedsSolDecisionMixedNoneTargetsCorrectsBeforeParentDispatch(t *testing.T) {
	st := newStateStoreT(t)
	decisionMixedNone := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":["none","glm-worker/internal/packet/validate.go:validateTargets"],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: decisionMixedNone},
		{structured: needsSolDecisionPacket()},
	}}
	var emitted bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &emitted)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-new" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[1], "意味検証に不合格") || !strings.Contains(r.prompts[1], "混在できません") {
		t.Fatalf("修正再依頼promptへ混在違反が伝わっていません: %s", r.prompts[1])
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("decision待ち状態へ遷移していません: status=%q pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if !strings.Contains(emitted.String(), `"status":"NEEDS_SOL_DECISION"`) || !strings.Contains(emitted.String(), `"targets":["t"]`) {
		t.Fatalf("親境界の結果packetへ修正後targetsが伝わっていません: %s", emitted.String())
	}
	if strings.Contains(emitted.String(), `"none"`) {
		t.Fatalf("none混在targetsが親境界へ流出しています: %s", emitted.String())
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
}

func TestNeedsSolReviewNoneElementCorrectsBeforeSolReviewDispatch(t *testing.T) {
	st := newStateStoreT(t)
	reviewMixedNone := `{"status":"NEEDS_SOL_REVIEW","risk":"HIGH","summary":"review","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["none","glm-worker/internal/packet/validate.go:validateTargets"],"artifacts":[],"sol_question":"q"}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: reviewMixedNone},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[2])
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"targets":["t"]`) || strings.Contains(review, "none") {
		t.Fatalf("Sol境界のreview packetへ正規targetsだけが伝わっていません: %s", review)
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 3 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}

func TestRiskFloorRejectsPassOnHighRiskWorker(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("HIGH risk workerへのreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if strings.Contains(review, "STATUS: PASS") {
		t.Fatalf("PASSが通っている: %s", review)
	}
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力NEEDS_SOL_REVIEWを採用すべき(捏造でない): %s", review)
	}
	if len(r.prompts) != 3 || !strings.Contains(r.prompts[2], "NEEDS_SOL_REVIEW (RISK: HIGH) だけ") {
		t.Fatalf("同一sessionへ再出力promptを送るべき: %#v", r.prompts)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("再出力もHighRiskReviewerModelを使うべき: %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterDecision(t *testing.T) {
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
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("decision applied", "LOW")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteDecision("A案で進める"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("decision後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("auto-fix後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,haiku,opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterExplicitFix(t *testing.T) {
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "previous review"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("explicit fix")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteExplicitFix("境界値を修正する", "", ""); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("explicit fix後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorRejectsPassAfterResume(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
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
		WorkerResult:   workerResultFromBody(`{"status":"IMPLEMENTED","risk":"HIGH","summary":"done","requirement_coverage":"covered","tests":"pass","unverified":"none"}`),
		ReviewNumber:   1,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resume後のreviewer PASSを拒否すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("risk floor強制packetでない: %s", review)
	}
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力を採用すべき: %s", review)
	}
	if strings.Join(r.models, ",") != "sonnet,sonnet" {
		t.Fatalf("resume後のreviewと再出力models = %#v", r.models)
	}
}

func TestRiskFloorAllowsLowRiskPass(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("LOW risk通常PASSは完遂すべき: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"PASS"`) || !strings.Contains(review, `"risk":"LOW"`) {
		t.Fatalf("PASS/LOWが保持されるべき: %s", review)
	}
	if strings.Join(r.models, ",") != "opus,haiku" {
		t.Fatalf("通常ReviewerModelを使うべき: %#v", r.models)
	}
}

func TestRiskFloorReemitFailClosedOnRepeatedPass(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: passPacket()},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再違反時はfail closedでSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, `"risk":"HIGH"`) {
		t.Fatalf("fail closed packetでない: %s", review)
	}
	if !strings.Contains(review, "PASS") {
		t.Fatalf("再違反のfail closed summaryは非許容STATUSを明示すべき: %s", review)
	}
	if strings.Contains(review, `"requirement_coverage":"covered"`) {
		t.Fatalf("reviewerのPASS内容を捏造してはいけない: %s", review)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("再出力は1回だけ行い無限反復しない: calls=%d", len(r.prompts))
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("fail closed後はresume checkpointを残さない")
	}
}

func TestRiskFloorReemitResumeCompliant(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           "reviewer-1-risk-floor",
		Role:            state.ReviewerRole,
		Model:           "sonnet",
		ReadOnly:        true,
		Effort:          "high",
		Prompt:          "reemit",
		OriginalPrompt:  "reemit",
		Request:         "request",
		WorkerResult:    workerResultFromBody(workerPacketWithRisk("HIGH")),
		ReviewNumber:    1,
		StopKind:        state.ResumeStopRateLimited,
		RiskFloorReemit: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{{structured: needsSolReviewPacket()}}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再出力resumeの準拠結果はSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"summary":"review"`) {
		t.Fatalf("reviewer自身の再出力NEEDS_SOL_REVIEWを採用すべき: %s", review)
	}
	if len(r.prompts) != 1 || !strings.Contains(r.prompts[0], "再開") {
		t.Fatalf("再出力工程からresume再開すべき: %#v", r.prompts)
	}
	if len(r.models) != 1 || r.models[0] != "sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestRiskFloorReemitResumeFailClosed(t *testing.T) {
	st := newStateStoreT(t)
	seedReviewStartSnapshot(t, st)
	if err := st.Write("last-request", "req"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:           state.ResumeStageReview,
		Phase:           "reviewer-1-risk-floor",
		Role:            state.ReviewerRole,
		Model:           "sonnet",
		ReadOnly:        true,
		Effort:          "high",
		Prompt:          "reemit",
		OriginalPrompt:  "reemit",
		Request:         "request",
		WorkerResult:    workerResultFromBody(workerPacketWithRisk("HIGH")),
		ReviewNumber:    1,
		StopKind:        state.ResumeStopRateLimited,
		RiskFloorReemit: true,
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
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("再出力resumeの再違反もfail closedでSol確認待ちへ: status=%q", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"status":"NEEDS_SOL_REVIEW"`) || !strings.Contains(review, "PASS") {
		t.Fatalf("fail closed packetでない: %s", review)
	}
	if strings.Contains(review, `"requirement_coverage":"covered"`) {
		t.Fatalf("reviewerのPASS内容を捏造してはいけない: %s", review)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("再出力resume後は追加呼出しない: calls=%d", len(r.prompts))
	}
}

func TestExecuteNewTaskPersistsParentCodexIdentityBeforeFirstDispatch(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{runErr: errors.New("worker stopped before terminal")},
	}}
	w := newWorkflowT(t, st, r)
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	t.Setenv(state.ParentActionCodexThreadIDEnv, threadID)
	t.Setenv(state.ParentActionCodexSessionIDEnv, threadID)

	_ = w.ExecuteNewTask("request")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != threadID || stats.ParentCodexSessionID != threadID {
		t.Fatalf("terminal前停止でもidentityが保存されている必要があります: %#v", stats)
	}
	if st.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("terminal前にtaskがcompleteになっています")
	}
}

func TestExecuteNewTaskWithoutParentActionIdentityLeavesStatsUnchanged(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{runErr: errors.New("worker stopped before terminal")},
	}}
	w := newWorkflowT(t, st, r)
	t.Setenv(state.ParentActionCodexThreadIDEnv, "")
	t.Setenv(state.ParentActionCodexSessionIDEnv, "")

	_ = w.ExecuteNewTask("request")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != "" || stats.ParentCodexSessionID != "" {
		t.Fatalf("直接glm-worker実行でidentityが書き込まれています: %#v", stats)
	}
}
