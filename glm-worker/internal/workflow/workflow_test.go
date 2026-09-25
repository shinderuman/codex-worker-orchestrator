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
		Targets:             []string{"glm-worker/internal/workflow/workflow_test.go:needsSolReviewPacket"},
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
	repoRoot := t.TempDir()
	gitIn(t, repoRoot, "init", "-q")
	trackRepositoryHarnessMarker(t, repoRoot)
	if st.TaskStatus() != state.TaskStatusActive {
		pinRepositoryHarnessActiveT(t, st)
	}
	w := NewWorkflow(config.AppConfig{
		WorkerModel:           "opus",
		ReviewerModel:         "haiku",
		HighRiskReviewerModel: "sonnet",
		RoutineEffort:         "high",
		MaxAutoFixRounds:      2,
		TelemetryContent:      true,
		RepoRoot:              repoRoot,
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
				Phase:   test.phase,
				Role:    test.role,
				ReadOnly: test.readOnly,
				Model:    "opus",
				Effort:   "high",
				Prompt:   "original",
				Request:  "request",
			})
			_ = err
		})
	}
}
