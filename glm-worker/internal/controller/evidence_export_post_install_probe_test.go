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

func exportProbeFixtureRecord(f *evidenceExportFixture) qualitygate.RunRecord {
	started := time.Unix(6100, 0).UTC()
	completed := started.Add(5 * time.Second)
	runID := "12121212121212121212121212121212"
	return qualitygate.RunRecord{
		ValidationRunID: runID, Form: smokeProbeForm,
		Repository: f.repo, WorkingDir: f.repo, TaskID: f.runtimeTask,
		Head: "source-oid", ProbeID: "runtime-status", ArgvDigest: "argv-digest",
		TaskAuthorityDigest: "task-contract", InstallEvidenceDigest: "install-proof",
		StartedAt: started, CompletedAt: &completed, Status: qualitygate.StatusPass,
		ExitCode: 0, ExitSource: state.ValidationExitSourceTarget, DurationMS: 5000,
		Log: f.runtime.Path(filepath.Join(qualitygate.RunDirectory, runID, "probe.log")),
	}
}

func writeExportProbeFixture(t *testing.T, f *evidenceExportFixture, record qualitygate.RunRecord, log []byte) {
	t.Helper()
	runDir := f.runtime.Path(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.RunFile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if log != nil {
		if err := os.WriteFile(filepath.Join(runDir, "probe.log"), log, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func exportProbeManifestPath(record qualitygate.RunRecord) string {
	return "live/validation/quality-gate/" + record.ValidationRunID + "/probe.log"
}

func findExportProbeAnalysis(t *testing.T, export EvidenceExport, record qualitygate.RunRecord) EvidenceAnalysisValidationRun {
	t.Helper()
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == record.ValidationRunID {
			return run
		}
	}
	t.Fatalf("analysis-index has no post-install probe run %s", record.ValidationRunID)
	return EvidenceAnalysisValidationRun{}
}

func TestExportPostInstallProbePreservesActualLogAndOtherValidationForms(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	record := exportProbeFixtureRecord(fixture)
	payload := []byte("post-install stdout\npost-install stderr\n")
	writeExportProbeFixture(t, fixture, record, payload)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entryPath := exportProbeManifestPath(record)
	entry := exportEntryByPath(t, export.Manifest, entryPath)
	if entry.Missing || entry.Unreadable != "" || entry.Changing || entry.Source != evidenceExportSourceGateRun ||
		entry.Basis != evidenceExportBasisRuntimeValidation || entry.Bytes != int64(len(payload)) || entry.SHA256 != digestBytes(payload) {
		t.Fatalf("post-install probe manifest entry = %#v", entry)
	}
	if string(exportFileByPath(t, export, entryPath)) != string(payload) {
		t.Fatal("bundled post-install probe log differs from actual output")
	}
	gatePrefix := "live/validation/quality-gate/" + record.ValidationRunID + "/"
	for _, bogus := range []string{qualitygate.RunLog, qualitygate.ResourceSamplesFile, qualitygate.TestTimingsFile} {
		if exportHasEntry(export.Manifest, gatePrefix+bogus) {
			t.Fatalf("post-install probe incorrectly requires Go artifact %s", bogus)
		}
	}
	analysisRun := findExportProbeAnalysis(t, export, record)
	if analysisRun.Form != smokeProbeForm || analysisRun.Result != qualitygate.StatusPass ||
		analysisRun.Log != entryPath || analysisRun.Binding != evidenceValidationBindingTaskRecord ||
		analysisRun.Basis != evidenceExportBasisRuntimeValidation || analysisRun.ResourceSamples != "" || analysisRun.TestTimings != "" {
		t.Fatalf("post-install probe provenance = %#v", analysisRun)
	}
	for _, existing := range []string{
		"live/validation/quality-gate/" + evidenceExportGateRunID + "/" + qualitygate.RunLog,
		"live/validation/install-smoke/" + evidenceExportSmokeRunID + "/" + evidenceExportValidationSmokeLog,
	} {
		if entry := exportEntryByPath(t, export.Manifest, existing); entry.Missing || entry.Unreadable != "" {
			t.Fatalf("ordinary validation evidence regressed: %#v", entry)
		}
	}
}

func TestExportPostInstallProbeRejectsUnboundOrTamperedLog(t *testing.T) {
	for _, tc := range []struct {
		name string
		mutate func(*qualitygate.RunRecord, *evidenceExportFixture)
		wantExportError bool
	}{
		{name: "foreign-task", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.TaskID = "foreign-task" }, wantExportError: true},
		{name: "missing-task", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.TaskID = "" }},
		{name: "foreign-repository", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.Repository = "/foreign/repository" }},
		{name: "different-working-dir", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.WorkingDir = "/foreign/workdir" }},
		{name: "external-log-path", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.Log = "/tmp/untrusted-probe.log" }},
		{name: "alternate-run-log", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.Log = filepath.Join(filepath.Dir(record.Log), qualitygate.RunLog) }},
		{name: "missing-probe-id", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.ProbeID = "" }},
		{name: "missing-install-identity", mutate: func(record *qualitygate.RunRecord, _ *evidenceExportFixture) { record.InstallEvidenceDigest = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newEvidenceExportFixture(t)
			record := exportProbeFixtureRecord(fixture)
			tc.mutate(&record, fixture)
			writeExportProbeFixture(t, fixture, record, []byte("must not be trusted\n"))
			export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if tc.wantExportError {
				if err == nil {
					t.Fatal("foreign task source passed export")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			entry := exportEntryByPath(t, export.Manifest, exportProbeManifestPath(record))
			if entry.Unreadable == "" || entry.SHA256 != "" || entry.Bytes != 0 {
				t.Fatalf("tampered probe became complete: %#v", entry)
			}
			if analysis := findExportProbeAnalysis(t, export, record); analysis.Log != "" {
				t.Fatalf("tampered probe log linked by analysis: %#v", analysis)
			}
		})
	}
}

func TestExportPostInstallProbeMissingAndSymlinkEvidence(t *testing.T) {
	for _, mode := range []string{"missing", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newEvidenceExportFixture(t)
			record := exportProbeFixtureRecord(fixture)
			writeExportProbeFixture(t, fixture, record, nil)
			if mode == "symlink" {
				outside := filepath.Join(t.TempDir(), "external-probe.log")
				if err := os.WriteFile(outside, []byte("foreign evidence"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, record.Log); err != nil {
					t.Fatal(err)
				}
			}
			export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
			if err != nil {
				t.Fatal(err)
			}
			entry := exportEntryByPath(t, export.Manifest, exportProbeManifestPath(record))
			if (mode == "missing" && !entry.Missing) || (mode == "symlink" && entry.Unreadable == "") || entry.SHA256 != "" {
				t.Fatalf("%s probe log evidence = %#v", mode, entry)
			}
			if run := findExportProbeAnalysis(t, export, record); run.Log != "" {
				t.Fatalf("%s probe log linked in analysis: %#v", mode, run)
			}
		})
	}
}
