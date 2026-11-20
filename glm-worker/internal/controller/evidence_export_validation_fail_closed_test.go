package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceDisclosesMissingValidationLogs(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	logless := qualitygate.RunRecord{
		ValidationRunID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Form: "go-test",
		Repository: fixture.repo, WorkingDir: fixture.repo,
		StartedAt: time.Unix(6100, 0).UTC(), Status: qualitygate.StatusPass,
		Log: "quality-gate-runs/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee/gate.log",
	}
	runData, err := json.Marshal(logless)
	if err != nil {
		t.Fatal(err)
	}
	runDir := fixture.runtime.Path(filepath.Join(qualitygate.RunDirectory, logless.ValidationRunID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.RunFile), runData, 0o600); err != nil {
		t.Fatal(err)
	}

	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	missingLog := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+logless.ValidationRunID+"/gate.log")
	if !missingLog.Missing || missingLog.Source != evidenceExportSourceGateRun {
		t.Fatalf("missing gate log entry = %#v", missingLog)
	}
	foundMissing := false
	for _, path := range export.Manifest.Runtime.Missing {
		if path == missingLog.Path {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("runtime section hides the missing gate log: %#v", export.Manifest.Runtime.Missing)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == logless.ValidationRunID && (run.Log != "" || run.Basis != evidenceExportBasisRuntimeValidation) {
			t.Fatalf("logless run analysis relation = %#v", run)
		}
	}
}

func TestExportEvidenceFailsClosedOnForeignGateRunInTaskStore(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	conflicting := qualitygate.RunRecord{
		ValidationRunID: "abababababababababababababababab", Form: "go-test",
		Repository: fixture.repo, WorkingDir: fixture.repo,
		StartedAt: time.Unix(6100, 0).UTC(), Status: qualitygate.StatusPass,
		TaskID: "another-runtime-task",
	}
	writeEvidenceGateRun(t, fixture.runtime, conflicting)
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export absorbed a gate run whose record is bound to another task")
	}
}

func TestExportEvidenceFailsClosedOnRuntimeRebinding(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	binding, err := fixture.runtime.LoadControllerRuntimeBinding()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: "attempt-other", TaskPath: fixture.seal.SemanticTaskRef.TaskPath,
		TaskContractDigest: fixture.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a runtime source rebound to another attempt")
	}
	if err := verifyExportRuntimeBindingStable(fixture.runtime, binding); err == nil {
		t.Fatal("binding stability check accepted a rebound runtime source")
	}
	if err := fixture.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: evidenceExportLiveAttemptID, TaskPath: "IMPLEMENTATION_TASKS/other.md",
		TaskContractDigest: fixture.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a runtime source rebound to another task")
	}
}
