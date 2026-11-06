package controller

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func exportProbeFixtureRecord(f *evidenceExportFixture) qualitygate.RunRecord {
	started := time.Unix(6100, 0).UTC()
	completed := started.Add(5 * time.Second)
	runID := "12121212121212121212121212121212"
	head := strings.Repeat("a", 40)
	workspace := filepath.Join(f.store.dir, "validation-"+head+"-"+smokeProbeForm)
	return qualitygate.RunRecord{
		ValidationRunID: runID, Form: smokeProbeForm,
		Repository: workspace, WorkingDir: workspace, TaskID: f.runtimeTask,
		Head: head, ProbeID: "runtime-status", ArgvDigest: "argv-digest",
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
	if record.LogSHA256 == "" {
		record.LogSHA256 = digestBytes(log)
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

func TestExportValidationPreservesGoArtifactsAndObservationClock(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	goRace := regressionFixtureRun(fixture.repo, "34343434343434343434343434343434", time.Unix(6150, 0).UTC())
	goRace.Form = "go-test-race"
	goRace.TaskID = fixture.runtimeTask
	writeEvidenceGateRun(t, fixture.runtime, goRace)
	future := time.Now().Add(time.Hour)
	runPath := fixture.runtime.Path(qualitygate.RunRelativePath(goRace.ValidationRunID))
	if err := os.Chtimes(runPath, future, future); err != nil {
		t.Fatal(err)
	}
	resources := gateRunResourceFixture(goRace.ValidationRunID)
	resources.Form = goRace.Form
	resourceBytes, err := qualitygate.EncodeResourceSamples(resources)
	if err != nil {
		t.Fatal(err)
	}
	timings := capturedFixtureTimings([]qualitygate.TestTiming{{Package: "example.com/m", Test: "TestSourceBound", Action: "pass", ElapsedMS: 100}})
	timings.Form = goRace.Form
	timings.ValidationRunID = goRace.ValidationRunID
	timingBytes, err := qualitygate.EncodeTestTimings(timings)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		qualitygate.ResourceSamplesFile: resourceBytes,
		qualitygate.TestTimingsFile:     timingBytes,
	} {
		if err := os.WriteFile(fixture.runtime.Path(filepath.Join(qualitygate.RunDirectory, goRace.ValidationRunID, name)), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+goRace.ValidationRunID+"/"+qualitygate.RunFile)
	if !entry.Changing {
		t.Fatal("観測開始後のGo gate recordが更新中と記録されていません")
	}
	for _, existing := range []string{
		"live/validation/quality-gate/" + evidenceExportGateRunID + "/" + qualitygate.RunLog,
		"live/validation/install-smoke/" + evidenceExportSmokeRunID + "/" + evidenceExportValidationSmokeLog,
		"live/validation/quality-gate/" + goRace.ValidationRunID + "/" + qualitygate.RunLog,
		"live/validation/quality-gate/" + goRace.ValidationRunID + "/" + qualitygate.ResourceSamplesFile,
		"live/validation/quality-gate/" + goRace.ValidationRunID + "/" + qualitygate.TestTimingsFile,
	} {
		if entry := exportEntryByPath(t, export.Manifest, existing); entry.Missing || entry.Unreadable != "" {
			t.Fatalf("ordinary validation evidence regressed: %#v", entry)
		}
	}
	goRaceRun := findExportProbeAnalysis(t, export, goRace)
	if goRaceRun.Form != "go-test-race" || goRaceRun.Log == "" || goRaceRun.ResourceSamples == "" ||
		goRaceRun.TestTimings == "" || goRaceRun.Binding != evidenceValidationBindingTaskRecord {
		t.Fatalf("Go race validation lost source-bound artifacts: %#v", goRaceRun)
	}
}

func TestExportPostInstallProbeRejectsSymlinkedRunRecord(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	record := exportProbeFixtureRecord(fixture)
	writeExportProbeFixture(t, fixture, record, []byte("real probe"))
	runPath := fixture.runtime.Path(qualitygate.RunRelativePath(record.ValidationRunID))
	backup := runPath + ".original"
	if err := os.Rename(runPath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backup, runPath); err != nil {
		t.Fatal(err)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	recordEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+record.ValidationRunID+"/"+qualitygate.RunFile)
	if recordEntry.Unreadable == "" || exportHasEntry(export.Manifest, exportProbeManifestPath(record)) {
		t.Fatalf("symlinked run.json supplied trusted probe evidence: %#v", recordEntry)
	}
}

func TestReadValidationRunFileRejectsReplacementDuringRead(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	record := exportProbeFixtureRecord(fixture)
	writeExportProbeFixture(t, fixture, record, []byte("original probe bytes"))
	_, _, err := readValidationRunFileWithRead(fixture.runtime, record.ValidationRunID, "probe.log", func(file *os.File) ([]byte, error) {
		original, readErr := io.ReadAll(file)
		if readErr != nil {
			return nil, readErr
		}
		replacement := record.Log + ".replacement"
		if writeErr := os.WriteFile(replacement, []byte("replacement probe bytes"), 0o600); writeErr != nil {
			return nil, writeErr
		}
		if renameErr := os.Rename(replacement, record.Log); renameErr != nil {
			return nil, renameErr
		}
		return original, nil
	})
	if err == nil {
		t.Fatal("validation evidence read accepted a file replaced during collection")
	}
}

func TestReadValidationRunFileRejectsDirectoryReplacementDuringRead(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	record := exportProbeFixtureRecord(fixture)
	writeExportProbeFixture(t, fixture, record, []byte("original probe bytes"))
	dir := filepath.Dir(record.Log)
	_, _, err := readValidationRunFileWithRead(fixture.runtime, record.ValidationRunID, "probe.log", func(file *os.File) ([]byte, error) {
		if err := os.Rename(dir, dir+".original"); err != nil {
			return nil, err
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			return nil, err
		}
		return io.ReadAll(file)
	})
	if err == nil || !strings.Contains(err.Error(), "directory changed") {
		t.Fatalf("置換されたrun directoryを受理しました: %v", err)
	}
}
