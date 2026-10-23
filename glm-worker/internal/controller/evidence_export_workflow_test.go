package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func workflowValidationSummary(workflow EvidenceAnalysisWorkflow, form string) (EvidenceAnalysisValidationSummary, bool) {
	for _, summary := range workflow.Validations {
		if summary.Form == form {
			return summary, true
		}
	}
	return EvidenceAnalysisValidationSummary{}, false
}

func workflowComparisonByMetric(regression EvidenceAnalysisWorkflowRegression, metric string) (EvidenceAnalysisWorkflowComparison, bool) {
	for _, comparison := range regression.Comparisons {
		if comparison.Metric == metric {
			return comparison, true
		}
	}
	return EvidenceAnalysisWorkflowComparison{}, false
}

func overwriteLiveTelemetry(t *testing.T, fixture *evidenceExportFixture, calls ...state.ModelCallLog) {
	t.Helper()
	if err := fixture.runtime.AppendTaskLifecycle(state.TaskLifecycleRecord{TaskID: fixture.runtimeTask, Timestamp: time.Unix(6000, 0).UTC(), From: "none", To: "active"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, call := range calls {
		data, err := json.Marshal(call)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(fixture.runtime.ModelCallLogPath(fixture.runtimeTask), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func workflowTelemetryCall(fixture *evidenceExportFixture, callID string, role state.SessionRole, start time.Time, wallMS int64, inputTokens int64, resumed bool, retryOf string) state.ModelCallLog {
	return state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallID: callID, CallType: state.CallTypeTask,
		TaskID: fixture.runtimeTask, SessionID: "session-worker",
		StartedAt: start, CompletedAt: start.Add(time.Duration(wallMS) * time.Millisecond),
		Phase: "completed", Role: role, Outcome: "success",
		WallDurationMS: wallMS,
		TopLevelUsage:  state.TokenUsage{InputTokens: inputTokens, OutputTokens: inputTokens / 2},
		TopLevelTurns:  2, TotalCostUSD: 0.25,
		Resumed: resumed, RetryOf: retryOf,
	}
}

func TestExportEvidenceWorkflowObservesLiveStagesUsageAndValidations(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture,
		workflowTelemetryCall(fixture, "w1", state.WorkerRole, time.Unix(6100, 0).UTC(), 1500, 4000, true, ""),
		workflowTelemetryCall(fixture, "w2", state.WorkerRole, time.Unix(6200, 0).UTC(), 2500, 6000, false, "w1"),
		workflowTelemetryCall(fixture, "r1", state.ReviewerRole, time.Unix(6300, 0).UTC(), 500, 1000, false, ""),
	)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	analysis := decodeAnalysisIndex(t, export)
	workflow := analysis.Workflow
	if workflow.Status != evidenceAnalysisStatusCollected || workflow.Basis != evidenceAnalysisBasisWorkflow {
		t.Fatalf("workflow section = %#v", workflow)
	}
	if workflow.TaskClock.Status != evidenceAnalysisStatusCollected || !workflow.TaskClock.Start.Equal(time.Unix(6000, 0).UTC()) ||
		workflow.TaskClock.WallMS <= 0 || workflow.TaskClock.EndBasis != evidenceExportLiveEndBasis {
		t.Fatalf("task clock = %#v", workflow.TaskClock)
	}
	if len(workflow.ModelStages) != 2 {
		t.Fatalf("model stages = %#v", workflow.ModelStages)
	}
	reviewer, worker := workflow.ModelStages[0], workflow.ModelStages[1]
	if reviewer.Role != string(state.ReviewerRole) || reviewer.Calls != 1 || reviewer.WallMS != 500 || reviewer.SlowestMS != 500 {
		t.Fatalf("reviewer stage = %#v", reviewer)
	}
	if worker.Role != string(state.WorkerRole) || worker.Calls != 2 || worker.WallMS != 4000 || worker.SlowestMS != 2500 ||
		worker.Retried != 1 || worker.Interrupted != 0 {
		t.Fatalf("worker stage = %#v", worker)
	}
	gate, ok := workflowValidationSummary(workflow, "go-test")
	if !ok || gate.Runs != 1 || gate.Pass != 1 || gate.DurationMS != 1000 || gate.TimingsCaptured != 0 || gate.ToolchainsRecorded != 0 {
		t.Fatalf("go-test summary = %#v", gate)
	}
	smoke, ok := workflowValidationSummary(workflow, "install-smoke")
	if !ok || smoke.Runs != 1 || smoke.Pass != 1 || smoke.DurationMS != 2000 {
		t.Fatalf("install-smoke summary = %#v", smoke)
	}
	if workflow.Interactions.ReviewRounds != 0 || workflow.Interactions.ResumedCalls != 1 ||
		workflow.Interactions.RetriedCalls != 1 || workflow.Interactions.AutoFixes != 0 {
		t.Fatalf("interactions = %#v", workflow.Interactions)
	}
	if len(workflow.Usage) != 2 || workflow.Usage[0].Role != string(state.ReviewerRole) || workflow.Usage[1].Role != string(state.WorkerRole) {
		t.Fatalf("usage summaries = %#v", workflow.Usage)
	}
	if usage := workflow.Usage[1]; usage.Side != evidenceWorkflowUsageSide || usage.Calls != 2 || usage.InputTokens != 10000 ||
		usage.OutputTokens != 5000 || usage.Turns != 4 || usage.CostUSD != 0.5 || usage.Coverage != evidenceWorkflowUsageCoverage {
		t.Fatalf("worker usage summary = %#v", usage)
	}
	if workflow.Regression.Status != evidenceAnalysisStatusAbsent || workflow.Regression.Unknown != "no comparable predecessor attempt with sealed telemetry in this export" {
		t.Fatalf("workflow regression = %#v", workflow.Regression)
	}
}

func TestExportEvidenceWorkflowRegressionComparesAgainstSealedPredecessor(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	overwriteLiveTelemetry(t, fixture,
		workflowTelemetryCall(fixture, "w1", state.WorkerRole, time.Unix(6100, 0).UTC(), 2000, 3000, true, ""),
		workflowTelemetryCall(fixture, "w2", state.WorkerRole, time.Unix(6200, 0).UTC(), 3000, 7000, false, ""),
	)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	predecessorRun := regressionFixtureRun(repoRoot, "ccccccccccccccccccccccccccccccc1", fixture.seal.StartedAt.Add(30*time.Second))
	predecessorRun.DurationMS = 2000
	fixture.writeModuleValidationRun(t, predecessorRun)
	liveRun := regressionFixtureRun(repoRoot, "ddddddddddddddddddddddddddddddd2", time.Unix(6500, 0).UTC())
	liveRun.DurationMS = 6000
	fixture.writeModuleValidationRun(t, liveRun)

	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	analysis := decodeAnalysisIndex(t, export)
	workflow := analysis.Workflow
	if workflow.Status != evidenceAnalysisStatusCollected {
		t.Fatalf("workflow section = %#v", workflow)
	}
	gate, ok := workflowValidationSummary(workflow, "go-test")
	if !ok || gate.Runs != 2 || gate.Pass != 2 || gate.DurationMS != 7000 || gate.ToolchainsRecorded != 1 {
		t.Fatalf("go-test summary = %#v", gate)
	}
	sealedWindows := 0
	for _, section := range export.Manifest.Attempt.Predecessors {
		if section.AttemptID == evidenceExportSealedAttemptID && section.Window != nil {
			sealedWindows++
		}
	}
	if sealedWindows != 1 {
		t.Fatalf("sealed predecessor window sections = %#v", export.Manifest.Attempt.Predecessors)
	}
	regression := workflow.Regression
	if regression.Status != evidenceAnalysisStatusAbsent || regression.Unknown == "" || len(regression.Comparisons) != 0 {
		t.Fatalf("missing predecessor model conditions produced comparisons: %#v", regression)
	}

}

func TestExportEvidenceWorkflowRegressionKeepsUnrecordedToolchainsNotComparable(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	overwriteLiveTelemetry(t, fixture,
		workflowTelemetryCall(fixture, "w1", state.WorkerRole, time.Unix(6100, 0).UTC(), 100, 100, false, ""),
	)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	predecessorRun := regressionFixtureRun(repoRoot, "ccccccccccccccccccccccccccccccc3", fixture.seal.StartedAt.Add(30*time.Second))
	predecessorRun.Toolchain = nil
	predecessorRun.DurationMS = 2000
	fixture.writeModuleValidationRun(t, predecessorRun)
	liveRun := regressionFixtureRun(repoRoot, "ddddddddddddddddddddddddddddddd4", time.Unix(6500, 0).UTC())
	liveRun.Toolchain = nil
	liveRun.DurationMS = 6000
	fixture.writeModuleValidationRun(t, liveRun)

	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	regression := analysis.Workflow.Regression
	if len(regression.Comparisons) != 0 || regression.Unknown == "" {
		t.Fatalf("unknown predecessor configuration was compared: %#v", regression)
	}

}

func TestExportEvidenceWorkflowTaskClockFollowsAnalysisWindow(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture,
		workflowTelemetryCall(fixture, "w1", state.WorkerRole, time.Unix(6100, 0).UTC(), 1000, 1000, false, ""),
	)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	analysis := decodeAnalysisIndex(t, export)
	if analysis.Window.Start.IsZero() {
		t.Fatalf("analysis window = %#v", analysis.Window)
	}
	if !analysis.Workflow.TaskClock.Start.Equal(analysis.Window.Start) {
		t.Fatalf("workflow task clock start %#v diverged from the analysis window %#v", analysis.Workflow.TaskClock.Start, analysis.Window.Start)
	}
	if qualitygate.TestTimingsSchemaVersion != 2 {
		t.Fatal("workflow observability expects the strict test timings schema")
	}
}
