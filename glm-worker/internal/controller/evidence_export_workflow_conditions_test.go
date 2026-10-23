package controller

import (
	"encoding/json"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkflowRegressionRequiresSameAuthorityAndModels(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/work.md", ContractDigest: "contract"}
	baseline := state.ModelCallLog{Version: state.ModelCallLogVersion, CallID: "baseline", CallType: state.CallTypeTask, ModelAlias: "glm-5.3", Effort: "high", Role: state.WorkerRole, StartedAt: start, CompletedAt: start.Add(time.Second), WallDurationMS: 1000, TopLevelUsage: state.TokenUsage{InputTokens: 1000}}
	wire, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	b := &evidenceExportBuilder{files: []EvidenceExportFile{{Path: "previous/telemetry.jsonl", Data: wire}}, attemptSection: EvidenceExportAttemptSection{Predecessors: []EvidenceExportPredecessorSection{{AttemptID: "baseline", Task: task, EntriesPrefix: "previous", Window: &EvidenceExportWindow{Start: start, End: start.Add(time.Minute)}}}}}
	latestCall := baseline
	latestCall.CallID = "latest"
	latestCall.StartedAt = start.Add(time.Minute)
	latestCall.CompletedAt = latestCall.StartedAt.Add(3 * time.Second)
	latestCall.WallDurationMS = 3000
	latestCall.TopLevelUsage.InputTokens = 3000
	attemptWindow := &EvidenceAnalysisWindow{Start: latestCall.StartedAt, End: latestCall.StartedAt.Add(3 * time.Minute)}
	latest := &workflowObservation{attemptWindow: attemptWindow, task: task, telemetry: []state.ModelCallLog{latestCall}, window: &EvidenceAnalysisWindow{Start: latestCall.StartedAt, End: latestCall.StartedAt.Add(3 * time.Minute)}}
	result := b.workflowRegression(latest)
	for _, metric := range []string{"attempt_wall_ms", "model:worker:wall_ms", "usage:worker:input_tokens"} {
		candidate, ok := workflowComparisonByMetric(result, metric)
		if !ok || candidate.Ratio != 3 {
			t.Fatalf("controlled regression %s: %#v", metric, result)
		}
	}
	for _, change := range []string{"model", "authority", "missing"} {
		copy := *latest
		copy.telemetry = append([]state.ModelCallLog(nil), latest.telemetry...)
		switch change {
		case "model":
			copy.telemetry[0].ModelAlias = "different"
		case "authority":
			copy.task.ContractDigest = "different"
		case "missing":
			copy.telemetry[0].Effort = ""
		}
		unknown := b.workflowRegression(&copy)
		if len(unknown.Comparisons) != 0 || unknown.Unknown == "" {
			t.Fatalf("%s compared incompatible observations: %#v", change, unknown)
		}
	}
}

func TestWorkflowModelWallClockUnionsOverlappingCalls(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	calls := []state.ModelCallLog{{CallType: state.CallTypeTask, Role: state.WorkerRole, StartedAt: start, CompletedAt: start.Add(2 * time.Second), WallDurationMS: 2000}, {CallType: state.CallTypeTask, Role: state.WorkerRole, StartedAt: start.Add(time.Second), CompletedAt: start.Add(3 * time.Second), WallDurationMS: 2000}}
	stages := workflowModelStages(calls)
	if len(stages) != 1 || stages[0].WallMS != 3000 || stages[0].WorkMS != 4000 {
		t.Fatalf("overlap was summed as a wall clock: %#v", stages)
	}
}

func TestWorkflowZeroBaselineCountIncreaseIsCandidateWithoutInventedRatio(t *testing.T) {
	candidate := workflowComparison("review_rounds", 5, 0, 2, "controlled fixture")
	if !candidate.Candidate || candidate.Ratio != 0 || candidate.Delta != 5 {
		t.Fatalf("zero baseline: %#v", candidate)
	}
	unknown := workflowComparison("review_rounds", 5, 0, workflowNotComparableFloor, "unknown")
	if unknown.Candidate {
		t.Fatal("unknown comparison produced a candidate")
	}
}

func TestWorkflowToolchainsDoNotSkipUnrecordedRuns(t *testing.T) {
	recorded := collectedValidationRun{run: regressionFixtureRun("repo", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Unix(1, 0))}
	missing := recorded
	missing.run.Toolchain = nil
	if workflowToolchainsComparable([]collectedValidationRun{recorded, missing}, []collectedValidationRun{recorded}, "go-test") {
		t.Fatal("mixed coverage was treated as fully comparable")
	}
}

func TestWorkflowUnknownDurationsAreNotComparable(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	observation := &workflowObservation{telemetry: []state.ModelCallLog{{CallType: state.CallTypeTask, Role: state.WorkerRole, WallDurationMS: 1000}}}
	if observation.modelWallMS("worker").observed {
		t.Fatal("missing model timestamps observed")
	}
	stages := workflowStatusStages([]state.TaskLifecycleRecord{{Timestamp: start}}, nil)
	if len(stages) != 1 || stages[0].DurationMS != nil || stages[0].Unknown == "" {
		t.Fatalf("missing stage end: %#v", stages)
	}
	stages = workflowStatusStages([]state.TaskLifecycleRecord{{Timestamp: start}}, &EvidenceAnalysisWindow{End: start})
	if stages[0].DurationMS == nil || *stages[0].DurationMS != 0 {
		t.Fatal("observed zero lost")
	}
	observation.runs = []collectedValidationRun{{run: qualitygate.RunRecord{Form: "go-test", StartedAt: start, Status: qualitygate.StatusRunning}}}
	if observation.validationDurationMS("go-test").observed {
		t.Fatal("unfinished validation duration observed")
	}
	summaries := workflowValidationSummaries(observation.runs)
	if summaries[0].UnknownDurations != 1 {
		t.Fatal("unknown validation duration not exposed")
	}
}

func TestWorkflowMalformedJSONLCannotBecomeZeroOrPartialBaseline(t *testing.T) {
	call := state.ModelCallLog{Version: state.ModelCallLogVersion, CallID: "one"}
	valid, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseWorkflowTelemetry(append(valid, []byte("\n{broken")...), true); got != nil {
		t.Fatal("partial telemetry admitted")
	}
	if got := parseWorkflowRounds([]byte("{broken"), true); got != nil {
		t.Fatal("malformed rounds became zero")
	}
	if got := parseWorkflowRounds(nil, true); got == nil || len(got) != 0 {
		t.Fatal("observed empty rounds lost")
	}
}

func TestGateRunTimingsRejectsDifferentMode(t *testing.T) {
	runID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	timings := capturedFixtureTimings([]qualitygate.TestTiming{{Package: "example/pkg", Test: "TestOne", Action: "pass", ElapsedMS: 1000}})
	path := filepath.Join(t.TempDir(), "timings.json")
	if err := os.WriteFile(path, []byte(mustEncodeTestTimings(t, runID, timings)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGateRunTimings(path, runID, "go-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := readGateRunTimings(path, runID, "go-test-race"); err == nil {
		t.Fatal("cross-mode timings admitted")
	}
}

func TestWorkflowObservesValidationWithoutWorkerTelemetry(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	result := decodeAnalysisIndex(t, export).Workflow
	if result.Status != evidenceAnalysisStatusCollected || result.TaskClock.Status != evidenceAnalysisStatusCollected || len(result.Validations) == 0 || len(result.Unknown) == 0 || len(result.ModelStages) != 0 || result.ParentUsage == nil {
		t.Fatalf("non-model observations lost: %#v", result)
	}
	if len(result.Regression.Comparisons) != 0 {
		t.Fatal("missing model conditions were compared")
	}
}
