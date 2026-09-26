package workflow

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
	w.collectReviewTargetPaths = func(string, string) ([]string, error) {
		return []string{"tracked.go"}, nil
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
