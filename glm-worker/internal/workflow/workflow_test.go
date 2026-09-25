package workflow

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
