package app

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func exportPostInstallProbeZIP(t *testing.T, cfg config.AppConfig, store *controller.Store) (controller.EvidenceExport, map[string][]byte) {
	t.Helper()
	export, result, err := store.ExportEvidence(controller.EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	path, err := writeEvidenceExportArchive(cfg, export, result.ArchiveName)
	if err != nil {
		t.Fatal(err)
	}
	verifyExportArchive(t, &controllerEvidenceExportOutput{ArchivePath: path, ManifestDigest: result.ManifestDigest})
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	members := make(map[string][]byte, len(archive.File))
	for _, entry := range archive.File {
		if _, exists := members[entry.Name]; exists {
			t.Fatalf("duplicate ZIP member %s", entry.Name)
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		payload, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read ZIP member %s: %v / %v", entry.Name, readErr, closeErr)
		}
		members[entry.Name] = payload
	}
	return export, members
}

func probeExportManifestEntry(t *testing.T, export controller.EvidenceExport, path string) controller.EvidenceExportEntry {
	t.Helper()
	for _, entry := range export.Manifest.Entries {
		if entry.Path == path {
			return entry
		}
	}
	t.Fatalf("Bundle manifest lacks %s", path)
	return controller.EvidenceExportEntry{}
}

func probeExportAnalysisRun(t *testing.T, members map[string][]byte, runID string) controller.EvidenceAnalysisValidationRun {
	t.Helper()
	var analysis controller.EvidenceAnalysisIndex
	if err := json.Unmarshal(members["analysis-index.json"], &analysis); err != nil {
		t.Fatal(err)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == runID {
			return run
		}
	}
	t.Fatalf("Bundle analysis-index lacks validation run %s", runID)
	return controller.EvidenceAnalysisValidationRun{}
}

func runRealPostInstallProbeForBundle(t *testing.T) (config.AppConfig, *controller.Store, *state.StateStore, qualitygate.RunRecord, []byte) {
	t.Helper()
	cfg, store, candidate, generation := appProbeFixture(t, []string{
		"/bin/sh", "-c", `printf 'probe stdout\\n'; printf 'probe stderr\\n' >&2`,
	})
	runtime := state.AttachStateStore(controller.WorkflowTaskConfig(cfg, store.Identity(), candidate.TaskRef))
	taskID := "producer-probe-bundle-task"
	if err := runtime.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          candidate.AttemptID,
		TaskPath:           candidate.TaskRef.TaskPath,
		RequestID:          candidate.TaskRef.RequestID,
		TaskContractDigest: candidate.TaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	out, err := executeParentCompletionPostInstallSmoke(cfg, store,
		controller.PublicationInput{ExpectedGeneration: generation, CandidateID: candidate.CandidateID},
		controllerPublicationCommand{SourceOID: candidate.CommitOID})
	if err != nil {
		t.Fatal(err)
	}
	output, ok := out.(postInstallSmokeOutput)
	if !ok || output.Result != "pass" || len(output.ProbeRuns) != 1 {
		t.Fatalf("post-install probe producer result = %#v", out)
	}
	record := latestAppProbeRun(t, cfg, store, candidate, "docker-worker-path")
	actual, err := os.ReadFile(record.Log)
	if err != nil {
		t.Fatal(err)
	}
	requirePostInstallProbeProducerRecord(t, record, taskID, actual)
	return cfg, store, runtime, record, actual
}

func requirePostInstallProbeProducerRecord(t *testing.T, record qualitygate.RunRecord, taskID string, actual []byte) {
	t.Helper()
	if record.TaskID != taskID || record.Form != controller.PublicationOperationPostInstallSmoke ||
		record.Status != qualitygate.StatusPass || record.LogSHA256 == "" {
		t.Fatalf("producer did not persist a source-bound PASS run: %#v", record)
	}
	if string(actual) != "probe stdout\nprobe stderr\n" {
		t.Fatalf("probe output differs: %q", actual)
	}
	sum := sha256.Sum256(actual)
	if record.LogSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("producer log digest = %s, expected %x", record.LogSHA256, sum)
	}
}

func checkRealPostInstallProbeZIP(t *testing.T, export controller.EvidenceExport, members map[string][]byte, record qualitygate.RunRecord, actual []byte) {
	t.Helper()
	prefix := "live/validation/quality-gate/" + record.ValidationRunID + "/"
	logEntry := prefix + "probe.log"
	if string(members[logEntry]) != string(actual) {
		t.Fatalf("Bundle ZIP probe log differs from executed process: %q", members[logEntry])
	}
	entry := probeExportManifestEntry(t, export, logEntry)
	sum := sha256.Sum256(actual)
	if entry.Missing || entry.Unreadable != "" || entry.Changing ||
		entry.Source != "quality-gate-run" || entry.Basis != "runtime-validation-store" {
		t.Fatalf("producer Bundle log provenance mismatch: %#v", entry)
	}
	if entry.Bytes != int64(len(actual)) || entry.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("producer Bundle size/SHA mismatch: %#v", entry)
	}
	checkBundledProbeRunRecord(t, members, prefix, record)
	checkBundledProbeAnalysis(t, export, members, prefix, record)
}

func checkBundledProbeRunRecord(t *testing.T, members map[string][]byte, prefix string, record qualitygate.RunRecord) {
	t.Helper()
	var bundledRecord qualitygate.RunRecord
	if err := json.Unmarshal(members[prefix+qualitygate.RunFile], &bundledRecord); err != nil {
		t.Fatal(err)
	}
	if bundledRecord.ValidationRunID != record.ValidationRunID || bundledRecord.Log != record.Log ||
		bundledRecord.LogSHA256 != record.LogSHA256 || bundledRecord.TaskID != record.TaskID {
		t.Fatalf("Bundle run record differs from producer: %#v", bundledRecord)
	}
}

func checkBundledProbeAnalysis(t *testing.T, export controller.EvidenceExport, members map[string][]byte, prefix string, record qualitygate.RunRecord) {
	t.Helper()
	logEntry := prefix + "probe.log"
	analysis := probeExportAnalysisRun(t, members, record.ValidationRunID)
	if analysis.Form != record.Form || analysis.Result != qualitygate.StatusPass ||
		analysis.Log != logEntry || analysis.Binding != "task-record" ||
		analysis.Basis != "runtime-validation-store" || analysis.ResourceSamples != "" || analysis.TestTimings != "" {
		t.Fatalf("producer Bundle analysis attribution mismatch: %#v", analysis)
	}
	for _, unexpected := range []string{qualitygate.RunLog, qualitygate.ResourceSamplesFile, qualitygate.TestTimingsFile} {
		for _, entry := range export.Manifest.Entries {
			if entry.Path == prefix+unexpected {
				t.Fatalf("post-install probe emitted Go-only artifact %s: %#v", unexpected, entry)
			}
		}
	}
}

func TestPostInstallSmokeProducerToBundleZIP(t *testing.T) {
	cfg, store, runtime, record, actual := runRealPostInstallProbeForBundle(t)
	prefix := "live/validation/quality-gate/" + record.ValidationRunID + "/"
	logEntry := prefix + "probe.log"
	export, members := exportPostInstallProbeZIP(t, cfg, store)
	checkRealPostInstallProbeZIP(t, export, members, record, actual)

	t.Run("modified-probe-log", func(t *testing.T) {
		if err := os.WriteFile(record.Log, []byte("tampered probe output\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		invalid, invalidMembers := exportPostInstallProbeZIP(t, cfg, store)
		probeExportAssertIncomplete(t, invalid, invalidMembers, logEntry, record.ValidationRunID, false)
	})
	t.Run("missing-probe-log", func(t *testing.T) {
		if err := os.Remove(record.Log); err != nil {
			t.Fatal(err)
		}
		invalid, invalidMembers := exportPostInstallProbeZIP(t, cfg, store)
		probeExportAssertIncomplete(t, invalid, invalidMembers, logEntry, record.ValidationRunID, true)
	})
	t.Run("legacy-missing-digest", func(t *testing.T) {
		if err := os.WriteFile(record.Log, actual, 0o600); err != nil {
			t.Fatal(err)
		}
		record.LogSHA256 = ""
		if err := writePostInstallSmokeRecord(runtime, record); err != nil {
			t.Fatal(err)
		}
		invalid, invalidMembers := exportPostInstallProbeZIP(t, cfg, store)
		probeExportAssertIncomplete(t, invalid, invalidMembers, logEntry, record.ValidationRunID, false)
	})
}

func probeExportAssertIncomplete(t *testing.T, export controller.EvidenceExport, members map[string][]byte, path, runID string, missing bool) {
	t.Helper()
	entry := probeExportManifestEntry(t, export, path)
	if (missing && !entry.Missing) || (!missing && entry.Unreadable == "") ||
		entry.SHA256 != "" || entry.Bytes != 0 {
		t.Fatalf("invalid post-install probe log appears complete: %#v", entry)
	}
	if _, exists := members[path]; exists {
		t.Fatalf("invalid probe log included as ZIP payload: %s", path)
	}
	if run := probeExportAnalysisRun(t, members, runID); run.Log != "" {
		t.Fatalf("invalid producer probe remains source-linked: %#v", run)
	}
}
