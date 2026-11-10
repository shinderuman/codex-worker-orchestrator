package app

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// assertTypedPostInstallBundleFidelity checks the same CLI-driven producer and
// exporter used by normal completion, rather than constructing a RunRecord.
func assertTypedPostInstallBundleFidelity(t *testing.T, cfg config.AppConfig, runtime *state.StateStore) {
	t.Helper()
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.MetadataLineageRef == nil {
		t.Fatalf("completed Task has no terminal lineage: %v", err)
	}
	terminal, err := store.LoadTerminalTaskRecord(*head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(terminal.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	record := latestAppProbeRun(t, cfg, store, candidate, "typed-worker-path")
	deployment, err := store.PostInstallSmokeDeployment(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID == "" || record.TaskID != runtime.ReadOr("task.id", "") ||
		record.Head != candidate.CommitOID || record.TaskAuthorityDigest != candidate.TaskRef.ContractDigest ||
		record.InstallEvidenceDigest != deployment.EvidenceDigest ||
		record.Status != qualitygate.StatusPass || record.LogSHA256 == "" ||
		deployment.SourceOID != candidate.CommitOID ||
		filepath.Base(deployment.BinaryPath) != "glm-worker" {
		t.Fatalf("CLI probe lost source/Task/deployment binding: %#v, deployment=%#v", record, deployment)
	}
	payload, err := os.ReadFile(record.Log)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "typed-probe-live-evidence" ||
		record.LogSHA256 != hex.EncodeToString(sha256Bytes(payload)) {
		t.Fatalf("CLI probe did not persist its actual output/digest: %#v, %q", record, payload)
	}
	manifest, members := typedPostInstallExportFromParentCLI(t, cfg, record.TaskID)
	if manifest.RepositoryIdentity != store.Identity().LineageID ||
		manifest.Target.RuntimeTaskID != record.TaskID ||
		manifest.Target.AttemptID != candidate.AttemptID ||
		!manifest.Target.Task.Equal(candidate.TaskRef) {
		t.Fatalf("CLI export selected a different Task/Attempt/source: %#v", manifest.Target)
	}
	checkRealPostInstallProbeZIP(t, controller.EvidenceExport{Manifest: manifest}, members, record, payload)

	// A later export must not silently turn a tampered probe into a complete log.
	if err := os.WriteFile(record.Log, []byte("tampered output"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid, invalidMembers := typedPostInstallExportFromParentCLI(t, cfg, record.TaskID)
	probePath := "bound/validation/quality-gate/" + record.ValidationRunID + "/probe.log"
	entry := probeExportManifestEntry(t, controller.EvidenceExport{Manifest: invalid}, probePath)
	if entry.Unreadable == "" || entry.SHA256 != "" {
		t.Fatalf("modified probe was accepted as complete: %#v", entry)
	}
	if _, exists := invalidMembers[probePath]; exists {
		t.Fatalf("modified probe log was exported as trusted bytes: %s", probePath)
	}
	analysis := probeExportAnalysisRun(t, invalidMembers, record.ValidationRunID)
	if analysis.Log != "" || analysis.ResourceSamples != "" || analysis.TestTimings != "" {
		t.Fatalf("tampered probe was attributed false artifacts: %#v", analysis)
	}
}

func typedPostInstallExportFromParentCLI(t *testing.T, cfg config.AppConfig, taskID string) (controller.EvidenceExportManifest, map[string][]byte) {
	t.Helper()
	exit, stdout, stderr := runTypedParentActionCLI(t, cfg, "export-bundle", "--task-id", taskID)
	if exit != 0 {
		t.Fatalf("production parent export command failed: %s", stderr)
	}
	var response struct {
		Action         string `json:"action"`
		ArchivePath    string `json:"archive_path"`
		ManifestDigest string `json:"manifest_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatal(err)
	}
	if response.Action != "export-task-bundle" || response.ArchivePath == "" || response.ManifestDigest == "" {
		t.Fatalf("parent export returned incomplete authority result: %s", stdout)
	}
	members := readTypedPostInstallZIP(t, response.ArchivePath)
	data, ok := members["manifest.json"]
	if !ok || response.ManifestDigest != hex.EncodeToString(sha256Bytes(data)) {
		t.Fatalf("parent export manifest digest mismatch: %s", stdout)
	}
	var manifest controller.EvidenceExportManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest, members
}

func readTypedPostInstallZIP(t *testing.T, path string) map[string][]byte {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	members := make(map[string][]byte, len(archive.File))
	for _, member := range archive.File {
		if _, exists := members[member.Name]; exists {
			t.Fatalf("duplicate export ZIP member %s", member.Name)
		}
		reader, err := member.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: %v / %v", member.Name, readErr, closeErr)
		}
		members[member.Name] = data
	}
	return members
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
