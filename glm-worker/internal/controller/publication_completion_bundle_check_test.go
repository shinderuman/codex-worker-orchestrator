package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type finalBundleTamperCase struct {
	name     string
	tamper   func(*EvidenceAnalysisIndex, []EvidenceExportFile) []EvidenceExportFile
	expected string
}

func newFinalBundleObservationFixture(t *testing.T) (*evidenceExportFixture, EvidenceExport, []FinalValidationEvidence) {
	t.Helper()
	fixture := newEvidenceExportFixture(t)
	overwriteLiveTelemetry(t, fixture,
		workflowTelemetryCall(fixture, "w1", state.WorkerRole, time.Unix(6100, 0).UTC(), 1500, 4000, false, ""),
		workflowTelemetryCall(fixture, "w2", state.WorkerRole, time.Unix(6100, 500000000).UTC(), 1500, 4000, false, ""),
	)
	event := workflowTelemetryCall(fixture, "parent-event", "codex-independent-review", time.Unix(6200, 0).UTC(), 10, 0, false, "")
	event.CallType = state.CallTypeEvent
	event.ModelAlias = ""
	appendLiveTelemetry(t, fixture, event)
	if err := fixture.runtime.AppendTaskLifecycle(state.TaskLifecycleRecord{TaskID: fixture.runtimeTask, Timestamp: time.Unix(6900, 0).UTC(), From: "active", To: string(state.TaskStatusAwaitingParentCompletion)}); err != nil {
		t.Fatal(err)
	}
	writeRuntimeGateTimings(t, fixture, evidenceExportGateRunID, "go-test")
	failing := qualitygate.RunRecord{
		ValidationRunID: "99999999999999999999999999999999", Form: "go-test",
		Repository: fixture.repo, WorkingDir: filepath.Join(fixture.repo, "glm-worker"),
		StartedAt: time.Unix(7050, 0).UTC(), Status: qualitygate.StatusFail,
	}
	failedAt := failing.StartedAt.Add(500 * time.Millisecond)
	failing.CompletedAt, failing.DurationMS = &failedAt, 500
	writeEvidenceGateRun(t, fixture.runtime, failing)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, export, []FinalValidationEvidence{{Form: "go-test", ValidationRunID: evidenceExportGateRunID}}
}

func TestFinalBundleObservationAcceptsConsistentAggregate(t *testing.T) {
	t.Parallel()
	fixture, export, finalRuns := newFinalBundleObservationFixture(t)
	check, err := verifyFinalBundleObservation(fixture.runtimeTask, finalRuns, export)
	if err != nil {
		t.Fatalf("consistent Bundle observation was rejected: %v", err)
	}
	if !check.TaskClockStart.Equal(time.Unix(6000, 0).UTC()) || check.TaskClockBasis != workflowTaskClockBasis {
		t.Fatalf("observed Task clock = %s/%s/%s", check.TaskClockStatus, check.TaskClockBasis, check.TaskClockStart.Format(time.RFC3339Nano))
	}
	if check.TelemetryDigest == "" || check.LifecycleDigest == "" {
		t.Fatalf("observation receipt lost its evidence digests: %#v", check)
	}
	if !reflect.DeepEqual(check.ModelCallsByRole, map[string]int{string(state.WorkerRole): 2}) {
		t.Fatalf("model call aggregate = %#v", check.ModelCallsByRole)
	}
	view := bundleValidationView(check, "go-test")
	if view == nil || view.Runs != 2 || view.Pass != 1 || view.Fail != 1 || view.DurationMS != 1500 || view.WallMS != 100000 || view.UnknownDuration != 0 {
		t.Fatalf("go-test aggregate view = %#v", check.Validations)
	}
}

func runFinalBundleTamperCases(t *testing.T, export EvidenceExport, runtimeTaskID string, finalRuns []FinalValidationEvidence, cases []finalBundleTamperCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tampered := tamperFinalBundle(t, export, testCase.tamper)
			if _, err := verifyFinalBundleObservation(runtimeTaskID, finalRuns, tampered); err == nil || !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("observation check accepted the incident signature: %v", err)
			}
		})
	}
}

func TestFinalBundleObservationRejectsTaskClockAndModelLoss(t *testing.T) {
	t.Parallel()
	fixture, export, finalRuns := newFinalBundleObservationFixture(t)
	runFinalBundleTamperCases(t, export, fixture.runtimeTask, finalRuns, []finalBundleTamperCase{
		{
			name: "task clock truncated to the last attempt",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.TaskClock.Start = time.Unix(6500, 0).UTC()
				return files
			},
			expected: "Task clock starts at",
		},
		{
			name: "task clock derived from the attempt window",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.TaskClock.Basis = "attempt-window"
				return files
			},
			expected: "does not cover the whole Task lifecycle",
		},
		{
			name: "task clock wall time zeroed while its window is intact",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.TaskClock.WallMS = 0
				return files
			},
			expected: "records 0 ms while its window spans",
		},
		{
			name: "task clock end stretched into the unobserved future",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.TaskClock.End = index.Workflow.TaskClock.End.Add(time.Hour)
				index.Workflow.TaskClock.WallMS += int64(time.Hour / time.Millisecond)
				return files
			},
			expected: "while its basis",
		},
		{
			name: "task clock end basis disagrees with the lifecycle boundary",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.TaskClock.EndBasis = workflowTaskClockCompleteBasis
				return files
			},
			expected: "end basis",
		},
		{
			name: "non-model event counted as a model call",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				index.Workflow.ModelStages = append(index.Workflow.ModelStages, EvidenceAnalysisModelStage{Role: "codex-independent-review", Calls: 1})
				return files
			},
			expected: "without canonical model calls",
		},
		{
			name: "model wall time replaces the interval union with the work sum",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				for i := range index.Workflow.ModelStages {
					index.Workflow.ModelStages[i].WallMS = index.Workflow.ModelStages[i].WorkMS
				}
				return files
			},
			expected: "the known call intervals union to",
		},
	})
}

func TestFinalBundleObservationRejectsValidationAggregateLoss(t *testing.T) {
	t.Parallel()
	fixture, export, finalRuns := newFinalBundleObservationFixture(t)
	rewriteGoTestSummary := func(index *EvidenceAnalysisIndex, rewrite func(*EvidenceAnalysisValidationSummary)) {
		for i := range index.Workflow.Validations {
			if index.Workflow.Validations[i].Form == "go-test" {
				rewrite(&index.Workflow.Validations[i])
			}
		}
	}
	runFinalBundleTamperCases(t, export, fixture.runtimeTask, finalRuns, []finalBundleTamperCase{
		{
			name: "post-seal final gate dropped from the workflow aggregate",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				var kept []EvidenceAnalysisValidationSummary
				for _, summary := range index.Workflow.Validations {
					if summary.Form != "go-test" {
						kept = append(kept, summary)
					}
				}
				index.Workflow.Validations = kept
				return files
			},
			expected: "workflow aggregate omits form go-test",
		},
		{
			name: "final gate run missing from the canonical Bundle",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				var kept []EvidenceAnalysisValidationRun
				for _, run := range index.CanonicalValidationRuns {
					if run.RunID != evidenceExportGateRunID {
						kept = append(kept, run)
					}
				}
				index.CanonicalValidationRuns = kept
				return files
			},
			expected: "absent from the canonical run inventory",
		},
		{
			name: "validation run time dropped from the aggregate",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				rewriteGoTestSummary(index, func(summary *EvidenceAnalysisValidationSummary) { summary.DurationMS = 0 })
				return files
			},
			expected: "ms of go-test run time; the Task owns 1500",
		},
		{
			name: "failed run counted as passing in the aggregate",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				rewriteGoTestSummary(index, func(summary *EvidenceAnalysisValidationSummary) { summary.Pass, summary.Fail = 2, 0 })
				return files
			},
			expected: "counts 2 passing go-test runs; the Task owns 1",
		},
		{
			name: "duplicated run over-counted in the aggregate",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				rewriteGoTestSummary(index, func(summary *EvidenceAnalysisValidationSummary) { summary.Runs = 3 })
				return files
			},
			expected: "counts 3 go-test runs; the Task owns 2",
		},
		{
			name: "wall time replaces the interval union with the run-time sum",
			tamper: func(index *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				rewriteGoTestSummary(index, func(summary *EvidenceAnalysisValidationSummary) { summary.WallMS = summary.DurationMS })
				return files
			},
			expected: "the known run intervals union to",
		},
	})
}

func TestFinalBundleObservationRejectsUncapturedTimingEvidence(t *testing.T) {
	t.Parallel()
	fixture, export, finalRuns := newFinalBundleObservationFixture(t)
	runFinalBundleTamperCases(t, export, fixture.runtimeTask, finalRuns, []finalBundleTamperCase{
		{
			name: "timing locator kept while its evidence entry is missing",
			tamper: func(_ *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				var kept []EvidenceExportFile
				for _, file := range files {
					if !strings.HasSuffix(file.Path, "/"+qualitygate.TestTimingsFile) || !strings.Contains(file.Path, evidenceExportGateRunID) {
						kept = append(kept, file)
					}
				}
				return kept
			},
			expected: "lacks the test timing evidence entry",
		},
		{
			name: "timing evidence was never captured from test events",
			tamper: func(_ *EvidenceAnalysisIndex, files []EvidenceExportFile) []EvidenceExportFile {
				uncaptured := qualitygate.TestTimings{SchemaVersion: qualitygate.TestTimingsSchemaVersion, ValidationRunID: evidenceExportGateRunID, Form: "go-test", Coverage: qualitygate.TestTimingCoverage{Source: qualitygate.TestTimingSourcePlainLog}}
				data, err := qualitygate.EncodeTestTimings(uncaptured)
				if err != nil {
					t.Fatal(err)
				}
				for i, file := range files {
					if strings.HasSuffix(file.Path, "/"+qualitygate.TestTimingsFile) && strings.Contains(file.Path, evidenceExportGateRunID) {
						files[i].Data = data
					}
				}
				return files
			},
			expected: "was not captured from machine-readable test events",
		},
	})
}

func TestCompletionReadinessBindsFinalBundleObservation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "bundle observed implementation\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := acceptFinalValidationSource(t, fixture, policy, source, nil)
	run := seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	var check CompletionBundleCheck
	found := false
	for _, object := range bundle.Objects {
		if object.Ref.Kind != completionBundleCheckEvidenceKind {
			continue
		}
		found = true
		data, err := fixture.store.LoadEvidenceObject(object.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := decodeControllerJSON(data, &check); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("completion Bundle lacks the final Bundle observation receipt")
	}
	runtimeTaskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if check.RuntimeTaskID != runtimeTaskID || check.TaskClockBasis != workflowTaskClockBasis || check.TaskClockStatus != evidenceAnalysisStatusCollected {
		t.Fatalf("observation receipt = %#v", check)
	}
	if len(check.FinalRuns) != 1 || check.FinalRuns[0].ValidationRunID != run.ValidationRunID {
		t.Fatalf("observation receipt final runs = %#v", check.FinalRuns)
	}
	if !reflect.DeepEqual(check.ModelCallsByRole, map[string]int{string(state.WorkerRole): 1}) {
		t.Fatalf("observation receipt model calls = %#v", check.ModelCallsByRole)
	}
	view := bundleValidationView(check, "go-test")
	if view == nil || view.Runs != 1 || view.Pass != 1 || view.Fail != 0 || view.DurationMS != run.DurationMS || view.UnknownDuration != 0 {
		t.Fatalf("observation receipt go-test view = %#v", check.Validations)
	}
	if head.PendingTransitionID != "" {
		t.Fatal("verified observation publication left a pending transition")
	}
}

func TestPublishRefusesBundleObservationChangedAfterReadiness(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "stale bundle observation\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := acceptFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	retired, _ := prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
	runtimeTaskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.AppendTaskLifecycle(state.TaskLifecycleRecord{TaskID: runtimeTaskID, Timestamp: time.Now().UTC(), From: string(state.TaskStatusAwaitingParentCompletion), To: string(state.TaskStatusAwaitingParentCompletion)}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(retired.TransitionID); err == nil || !strings.Contains(err.Error(), "changed after readiness") {
		t.Fatalf("publication reused a Bundle observation prepared against earlier runtime evidence: %v", err)
	}
	assertPublicationRefusedWithoutRemoteMovement(t, fixture, policy, runtime)
}

func TestPublishRefusesBundleWithoutTelemetryEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "missing telemetry implementation\n")
	runtime := acceptedCompletionRuntimeOptions(t, fixture.store, source, false)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	runtimeTaskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(runtime.ModelCallLogPath(runtimeTaskID)); err != nil {
		t.Fatal(err)
	}
	promoted, candidate := acceptFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), "carries no worker telemetry evidence") {
		t.Fatalf("publication must refuse a final Task Bundle without the canonical telemetry source: %v", err)
	}
	assertPublicationRefusedWithoutRemoteMovement(t, fixture, policy, runtime)
}

func TestCompletionBundleCaptureRefusesRunSourcesChangedDuringObservation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "mid-observation run source change\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := acceptFinalValidationSource(t, fixture, policy, source, nil)
	run := seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	retired, prepared := prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
	var op ExecutionOperation
	if err := readJSON(fixture.store.executionOperationPath(retired.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	runtimeTaskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	before, err := fixture.store.completionBundleRunStores(op, prepared)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := fixture.store.observeCompletionBundle(op, prepared, runtimeTaskID, []FinalValidationEvidence{{Form: "go-test", ValidationRunID: run.ValidationRunID}})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := fixture.store.completionBundleRunStores(op, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := bindCompletionBundleSources(&observed, before, unchanged); err != nil {
		t.Fatalf("capture refused validation run sources that held still: %v", err)
	}
	if !reflect.DeepEqual(observed.RunStores, before) {
		t.Fatal("stable capture did not bind its run source frame into the receipt")
	}
	late := qualitygate.RunRecord{ValidationRunID: "88888888888888888888888888888888", Form: "go-test", StartedAt: time.Unix(7200, 0).UTC(), Status: qualitygate.StatusPass}
	completedAt := late.StartedAt.Add(time.Second)
	late.CompletedAt, late.DurationMS = &completedAt, 1000
	writeEvidenceGateRun(t, runtime, late)
	after, err := fixture.store.completionBundleRunStores(op, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := bindCompletionBundleSources(&observed, before, after); err == nil || !strings.Contains(err.Error(), "changed while the observation was captured") {
		t.Fatalf("capture accepted validation run sources that changed after the observed export: %v", err)
	}
}

func assertPublicationRefusedWithoutRemoteMovement(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, runtime *state.StateStore) {
	t.Helper()
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || remote != fixture.source.Head.IntegrationTip {
		t.Fatalf("refused publication still moved the remote: remote=%s error=%v", remote, err)
	}
	if runtime.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("refused publication completed the Runtime task")
	}
}

func tamperFinalBundle(t *testing.T, export EvidenceExport, tamper func(*EvidenceAnalysisIndex, []EvidenceExportFile) []EvidenceExportFile) EvidenceExport {
	t.Helper()
	data, ok := finalBundleFile(export, evidenceAnalysisIndexEntryPath)
	if !ok {
		t.Fatal("export lacks the analysis index")
	}
	var index EvidenceAnalysisIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	files := tamper(&index, export.Files)
	tampered, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tampered = append(tampered, '\n')
	withIndex := make([]EvidenceExportFile, 0, len(files))
	for _, file := range files {
		if file.Path == evidenceAnalysisIndexEntryPath {
			file.Data = tampered
		}
		withIndex = append(withIndex, file)
	}
	return EvidenceExport{Manifest: export.Manifest, Files: withIndex}
}

func bundleValidationView(check CompletionBundleCheck, form string) *CompletionBundleValidationView {
	for i := range check.Validations {
		if check.Validations[i].Form == form {
			return &check.Validations[i]
		}
	}
	return nil
}

func appendLiveTelemetry(t *testing.T, fixture *evidenceExportFixture, calls ...state.ModelCallLog) {
	t.Helper()
	var buf bytes.Buffer
	for _, call := range calls {
		data, err := json.Marshal(call)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	file := fixture.runtime.ModelCallLogPath(fixture.runtimeTask)
	current, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(current, buf.Bytes()...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeGateTimings(t *testing.T, fixture *evidenceExportFixture, runID, form string) {
	t.Helper()
	gateLog := []byte(`{"Action":"pass","Package":"example.com/fixture","Test":"TestObservedFixture","Elapsed":0.125}` + "\n")
	data, err := qualitygate.EncodeTestTimings(qualitygate.DistillTestTimings(runID, form, gateLog))
	if err != nil {
		t.Fatal(err)
	}
	path := fixture.runtime.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.TestTimingsFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
