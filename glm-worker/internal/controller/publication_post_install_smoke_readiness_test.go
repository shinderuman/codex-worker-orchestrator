package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPostInstallSmokeMissingRunDirectoryIsOutstanding(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "smoke-not-started.txt", "smoke input\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	_, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, candidate.TaskRef))
	runDir := module.Path(qualitygate.RunDirectory)
	if err := os.RemoveAll(runDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("fixture did not leave run directory absent: %v", err)
	}
	outstanding, err := fixture.store.OutstandingSmokeProbes(candidate)
	if err != nil || len(outstanding) != 1 || outstanding[0].ProbeID != dockerRoutingProbe().ProbeID {
		t.Fatalf("unstarted probe was not reported as outstanding: probes=%#v err=%v", outstanding, err)
	}
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	operation, err := fixture.store.ProjectParentCompletion()
	if err != nil || operation.Operation != PublicationOperationPostInstallSmoke {
		t.Fatalf("missing probe did not lead to canonical smoke operation: operation=%#v err=%v", operation, err)
	}
}

func TestPostInstallSmokeUnreadableRunDoesNotBecomeMissing(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "smoke-invalid-record.txt", "smoke record\n")
	_, candidate := acceptProbedCandidate(t, fixture, policy, source, []SmokeProbeDeclaration{dockerRoutingProbe()})
	record := seedSmokeProbeRun(t, fixture.store, candidate, dockerRoutingProbe(), qualitygate.StatusPass, nil)
	module, err := fixture.store.finalValidationRunStore(candidate.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(module.Path(qualitygate.RunDirectory), record.ValidationRunID, "run.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.store.OutstandingSmokeProbes(candidate)
	if err == nil || !strings.Contains(err.Error(), record.ValidationRunID) {
		t.Fatalf("unreadable probe run was treated as absent: %v", err)
	}
}

func TestPostInstallSmokeReadinessReaderMatchesValidationMissingSemantics(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t)
	module := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.identity, fixture.source.Attempt.SemanticTaskRef))
	if err := os.RemoveAll(module.Path(qualitygate.RunDirectory)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := latestSmokeProbeRun(module, t.TempDir(), "probe"); !os.IsNotExist(err) {
		t.Fatalf("raw smoke reader did not preserve the filesystem error: %v", err)
	}
	if _, _, err := latestFinalValidationRun(module, t.TempDir(), "go-test"); !os.IsNotExist(err) {
		t.Fatalf("raw validation reader did not preserve the filesystem error: %v", err)
	}
}
