package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportPostInstallProbePreservesReenteredCandidateFailure(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "probed.txt", "reviewed implementation\n")
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	accepted, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	record := seedSmokeProbeRun(t, fixture.store, candidate, dockerRoutingProbe(), qualitygate.StatusFail, func(run *qualitygate.RunRecord) {
		completed := time.Now().UTC()
		run.CompletedAt = &completed
		run.DurationMS = completed.Sub(run.StartedAt).Milliseconds()
		run.ExitCode = 1
		run.ExitSource = state.ValidationExitSourceTarget
	})
	_, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := fixture.store.ProjectParentCompletion()
	if err != nil || operation.Execution == nil || operation.Execution.Action != "materialize" {
		t.Fatalf("reentry projection: %#v %v", operation, err)
	}
	materialized, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: operation.Execution.ExpectedGeneration, SuspensionID: operation.Execution.SuspensionID})
	if err != nil || materialized.Admission == nil {
		t.Fatalf("materialize: %v", err)
	}
	builder := &evidenceExportBuilder{store: fixture.store, head: materialized.Head}
	collector := &evidenceLiveCollector{store: fixture.store, runtime: runtime, taskID: record.TaskID, attempt: materialized.Admission.Attempt, builder: builder, observedAt: time.Now().UTC()}
	if err := collector.collectGateRun(runtime, record.ValidationRunID, evidenceExportBasisRuntimeValidation, evidenceValidationBindingTaskStore, nil); err != nil {
		t.Fatal(err)
	}
	canonical, _ := builder.analysisValidationRuns()
	inventory, err := collectBundleValidationInventory([]qualitygate.RunRecord{record}, canonical, record.TaskID, nil)
	if err != nil || len(canonical) != 1 || canonical[0].Result != qualitygate.StatusFail || inventory.byForm[smokeProbeForm] == nil || inventory.byForm[smokeProbeForm].fail != 1 {
		t.Fatalf("historical probe failure was lost: %#v %#v %v", canonical, inventory, err)
	}
	for name, mutate := range map[string]func(*qualitygate.RunRecord){
		"source":     func(run *qualitygate.RunRecord) { run.Head = strings.Repeat("f", 40) },
		"contract":   func(run *qualitygate.RunRecord) { run.TaskAuthorityDigest = strings.Repeat("f", 64) },
		"deployment": func(run *qualitygate.RunRecord) { run.InstallEvidenceDigest = strings.Repeat("f", 64) },
		"argv":       func(run *qualitygate.RunRecord) { run.ArgvDigest = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := record
			mutate(&changed)
			if err := collector.verifyPostInstallProbeSource(runtime, changed, record.ValidationRunID); err == nil {
				t.Fatal("tampered historical probe was accepted")
			}
		})
	}
	collector.taskID = "00000000-0000-4000-8000-000000000001"
	changed := record
	changed.TaskID = collector.taskID
	if err := collector.verifyPostInstallProbeSource(runtime, changed, record.ValidationRunID); err == nil {
		t.Fatal("a candidate from another runtime Task was accepted")
	}
}

func TestExportPostInstallProbeParentOnlyRuntimeIdentity(t *testing.T) {
	fixture := newParentOnlyInstallSmokeFixture(t)
	head, err := fixture.fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	collector := &evidenceLiveCollector{store: fixture.fixture.store, taskID: fixture.candidate.AttemptID, builder: &evidenceExportBuilder{head: head}}
	if err := collector.verifyPostInstallProbeRuntime(fixture.candidate); err != nil {
		t.Fatalf("canonical parent-only runtime identity was rejected: %v", err)
	}
	collector.taskID = "00000000-0000-4000-8000-000000000001"
	if err := collector.verifyPostInstallProbeRuntime(fixture.candidate); err == nil {
		t.Fatal("another parent-only runtime Task was accepted")
	}
}
