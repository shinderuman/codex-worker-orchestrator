package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestWorkflowTaskScopeIncludesEarlierAttemptAndPostSealGates(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	start := fixture.seal.StartedAt.Add(-time.Minute)
	if err := fixture.runtime.AppendTaskLifecycle(state.TaskLifecycleRecord{TaskID: fixture.runtimeTask, Timestamp: start, From: "none", To: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.AppendTaskLifecycle(state.TaskLifecycleRecord{TaskID: fixture.runtimeTask, Timestamp: time.Unix(8000, 0).UTC(), From: "active", To: string(state.TaskStatusComplete)}); err != nil {
		t.Fatal(err)
	}
	repo, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	run := regressionFixtureRun(repo, "ccccccccccccccccccccccccccccccc5", time.Unix(7500, 0).UTC())
	run.TaskID = fixture.runtimeTask
	run.DurationMS = 3000
	fixture.writeModuleValidationRun(t, run)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	workflow := decodeAnalysisIndex(t, export).Workflow
	if !workflow.TaskClock.Start.Equal(start) || !workflow.TaskClock.End.Equal(time.Unix(8000, 0).UTC()) || workflow.TaskClock.EndBasis != "runtime-task-complete" || workflow.TaskClock.Basis != "runtime-task-lifecycle" {
		t.Fatalf("Task clock truncated: %#v", workflow.TaskClock)
	}
	summary, ok := workflowValidationSummary(workflow, "go-test")
	if !ok || summary.Runs != 2 || summary.DurationMS != 4000 {
		t.Fatalf("post-seal gate missing: %#v", workflow.Validations)
	}
	if len(workflow.ModelStages) != 0 {
		t.Fatalf("malformed telemetry counted: %#v", workflow.ModelStages)
	}
}

func TestWorkflowAttemptComparisonFiltersCumulativeTelemetryAndRounds(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	task := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/work.md", ContractDigest: "contract"}
	call := state.ModelCallLog{Version: state.ModelCallLogVersion, CallID: "baseline", TaskID: "task", CallType: state.CallTypeTask, Role: state.WorkerRole, ModelAlias: "glm-5.3", Effort: "high", StartedAt: start, CompletedAt: start.Add(time.Second), WallDurationMS: 1000, TopLevelUsage: state.TokenUsage{InputTokens: 1000}}
	later := call
	later.CallID = "latest"
	later.StartedAt, later.CompletedAt = start.Add(time.Minute), start.Add(time.Minute+3*time.Second)
	later.WallDurationMS, later.TopLevelUsage.InputTokens = 3000, 3000
	event := later
	event.CallID, event.CallType, event.Role, event.ModelAlias = "event", state.CallTypeEvent, "codex-independent-review", ""
	previousWire, err := json.Marshal(call)
	if err != nil {
		t.Fatal(err)
	}
	b := &evidenceExportBuilder{files: []EvidenceExportFile{{Path: "previous/telemetry.jsonl", Data: previousWire}}, attemptSection: EvidenceExportAttemptSection{Predecessors: []EvidenceExportPredecessorSection{{AttemptID: "baseline", Task: task, EntriesPrefix: "previous", Window: &EvidenceExportWindow{Start: start, End: start.Add(time.Minute)}}}}}
	attempt := &EvidenceAnalysisWindow{Start: later.StartedAt, End: later.StartedAt.Add(3 * time.Minute)}
	observation := &workflowObservation{task: task, window: &EvidenceAnalysisWindow{Start: start, End: attempt.End}, attemptWindow: attempt, telemetry: []state.ModelCallLog{call, later, event}, rounds: []state.RoundRecord{{CapturedAt: start}, {CapturedAt: later.StartedAt}}}
	stages := workflowModelStages(observation.telemetry)
	if len(stages) != 1 || stages[0].Calls != 2 || stages[0].WallMS != 4000 {
		t.Fatalf("events counted as calls: %#v", stages)
	}
	usage := workflowUsageSummaries(observation.telemetry)
	if len(usage) != 1 || usage[0].Calls != 2 || usage[0].InputTokens != 4000 {
		t.Fatalf("events counted as usage: %#v", usage)
	}
	result := b.workflowRegression(observation)
	for _, metric := range []string{"attempt_wall_ms", "model:worker:wall_ms", "usage:worker:input_tokens"} {
		comparison, ok := workflowComparisonByMetric(result, metric)
		if !ok || comparison.Ratio != 3 || !comparison.Candidate {
			t.Fatalf("wrong comparison scope for %s: %#v", metric, result)
		}
	}
	scoped := b.workflowAttemptObservation(observation)
	if len(scoped.rounds) != 1 {
		t.Fatalf("earlier review counted: %#v", scoped.rounds)
	}
	crossing := later
	crossing.CallID = "crossing"
	crossing.StartedAt = start.Add(59 * time.Second)
	observation.telemetry = append(observation.telemetry, crossing)
	if result := b.workflowRegression(observation); len(result.Comparisons) != 0 || result.Unknown == "" {
		t.Fatalf("crossing call guessed: %#v", result)
	}
}

func TestWorkflowTaskValidationDedupAndParallelWall(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	end := start.Add(2 * time.Second)
	a := collectedValidationRun{run: qualitygate.RunRecord{ValidationRunID: "a", TaskID: "task", Form: "go-test", StartedAt: start, CompletedAt: &end, Status: qualitygate.StatusPass, DurationMS: 2000}}
	other := a
	other.run.ValidationRunID = "b"
	other.run.StartedAt = start.Add(time.Second)
	laterEnd := end.Add(time.Second)
	other.run.CompletedAt = &laterEnd
	foreign := a
	foreign.run.ValidationRunID, foreign.run.TaskID = "foreign", "another-task"
	b := &evidenceExportBuilder{validationRuns: []collectedValidationRun{a, a, other, foreign}}
	runs := b.workflowTaskRuns("task", nil)
	summaries := workflowValidationSummaries(runs)
	if len(summaries) != 1 || summaries[0].Runs != 2 || summaries[0].DurationMS != 4000 || summaries[0].WallMS != 3000 {
		t.Fatalf("duplicate or overlapping gate: %#v", summaries)
	}
}

func TestWorkflowMissingTaskStartNeverUsesAttemptClock(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	workflow := decodeAnalysisIndex(t, export).Workflow
	if workflow.TaskClock.Status != evidenceAnalysisStatusAbsent || len(workflow.Validations) == 0 || len(workflow.Unknown) == 0 {
		t.Fatalf("missing Task clock concealed: %#v", workflow)
	}
}
