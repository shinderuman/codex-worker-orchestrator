package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type controllerEvidenceExportFixture struct {
	cfg      config.AppConfig
	store    *controller.Store
	runtime  *state.StateStore
	taskID   string
	sessions string
}

type exportManifestEntry struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	Missing    bool   `json:"missing,omitempty"`
	Unreadable string `json:"unreadable,omitempty"`
}

func newControllerEvidenceExportFixture(t *testing.T) *controllerEvidenceExportFixture {
	t.Helper()
	repo := t.TempDir()
	runControllerActivationGit(t, repo, "init", "-q")
	runControllerActivationGit(t, repo, "config", "user.email", "export@example.invalid")
	runControllerActivationGit(t, repo, "config", "user.name", "Export Test")
	writeAppTestFile(t, repo, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, repo, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\ncontroller export task\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, repo, "add", ".")
	runControllerActivationGit(t, repo, "commit", "-q", "-m", "base")

	stateBase := filepath.Join(t.TempDir(), "state", "sessions")
	hash := config.RepoHashFor(repo)
	cfg := config.AppConfig{RepoRoot: repo, RepoHash: hash, RepoShort: hash[:12], StateBase: stateBase}
	cfg.ClaudeConfigDir = t.TempDir()
	cfg.CodexConfigDir = t.TempDir()
	var activation bytes.Buffer
	if err := runEntry(
		[]string{"--authority", "controller-activate"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		bytes.NewReader(nil),
		&activation,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	var activated controllerActivationOutput
	if err := json.Unmarshal(activation.Bytes(), &activated); err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	workflowCfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := state.NewStateStore(workflowCfg)
	if err != nil {
		t.Fatal(err)
	}
	runtimeTask := "runtime-task-export-1"
	if err := runtime.Write("task.id", runtimeTask); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          activated.AttemptID,
		TaskPath:           activated.Task.TaskPath,
		TaskContractDigest: activated.Task.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("worker.id", "session-export-worker"); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(cfg.ClaudeConfigDir, "projects", "export")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "session-export-worker.jsonl"), []byte("{\"line\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	telemetryPath := runtime.ModelCallLogPath(runtimeTask)
	if err := os.MkdirAll(filepath.Dir(telemetryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(telemetryPath, []byte("{\"version\":3}\n{\"partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &controllerEvidenceExportFixture{cfg: cfg, store: store, runtime: runtime, taskID: runtimeTask, sessions: activated.AttemptID}
}

func runExportAction(t *testing.T, fixture *controllerEvidenceExportFixture, payload string, stdout io.Writer) error {
	t.Helper()
	return runEntry(
		[]string{"--authority", "controller-evidence"},
		func() (config.AppConfig, error) { return fixture.cfg, nil },
		nil,
		strings.NewReader(payload),
		stdout,
		io.Discard,
	)
}

func directoryTreeDigest(t *testing.T, root string) string {
	t.Helper()
	var builder strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		builder.WriteString(rel + "\x00" + hex.EncodeToString(sum[:]) + "\x00")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder.String()
}

func gitStatusSnapshot(t *testing.T, repo string) string {
	t.Helper()
	output, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestControllerEvidenceExportWritesArchiveWithoutMutatingAuthorities(t *testing.T) {
	fixture := newControllerEvidenceExportFixture(t)
	controllerDir := filepath.Join(filepath.Dir(fixture.cfg.StateBase), "controllers")
	controllerBefore := directoryTreeDigest(t, controllerDir)
	sessionsBefore := directoryTreeDigest(t, fixture.cfg.StateBase)
	statusBefore := gitStatusSnapshot(t, fixture.cfg.RepoRoot)

	var stdout bytes.Buffer
	if err := runExportAction(t, fixture, `{"action":"export-task-bundle"}`, &stdout); err != nil {
		t.Fatal(err)
	}
	var output controllerEvidenceOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("export stdout is not machine JSON: %v: %s", err, stdout.String())
	}
	if output.Action != controllerEvidenceExportBundle {
		t.Fatalf("export output action = %q", output.Action)
	}
	exported := output.Export
	if exported == nil || exported.ArchivePath == "" || exported.LiveSection != "collected" || exported.Coverage != "partial" {
		t.Fatalf("export result = %#v", exported)
	}
	if exported.Target.AttemptID != fixture.sessions || exported.Target.Live != true {
		t.Fatalf("export target = %#v", exported.Target)
	}
	verifyExportArchive(t, exported)

	if directoryTreeDigest(t, controllerDir) != controllerBefore {
		t.Fatal("evidence export mutated the controller store")
	}
	if directoryTreeDigest(t, fixture.cfg.StateBase) != sessionsBefore {
		t.Fatal("evidence export mutated worker runtime state")
	}
	if gitStatusSnapshot(t, fixture.cfg.RepoRoot) != statusBefore {
		t.Fatal("evidence export mutated repository Git state")
	}

	firstArchiveDigest := archiveFileDigest(t, exported.ArchivePath)
	var repeat bytes.Buffer
	if err := runExportAction(t, fixture, `{"action":"export-task-bundle"}`, &repeat); err != nil {
		t.Fatal(err)
	}
	var repeatOutput controllerEvidenceOutput
	if err := json.Unmarshal(repeat.Bytes(), &repeatOutput); err != nil {
		t.Fatal(err)
	}
	if repeatOutput.Export == nil || repeatOutput.Export.ArchivePath == exported.ArchivePath {
		t.Fatalf("repeated export reused archive path: %#v", repeatOutput.Export)
	}
	if _, err := os.Stat(repeatOutput.Export.ArchivePath); err != nil {
		t.Fatalf("repeated export archive is missing: %v", err)
	}
	if _, err := os.Stat(exported.ArchivePath); err != nil {
		t.Fatalf("previous export archive was removed by a repeated export: %v", err)
	}
	if archiveFileDigest(t, exported.ArchivePath) != firstArchiveDigest {
		t.Fatal("repeated export overwrote previous export bytes")
	}

	fixture.cfg.ClaudeConfigDir = t.TempDir()
	var isolated bytes.Buffer
	if err := runExportAction(t, fixture, `{"action":"export-task-bundle"}`, &isolated); err != nil {
		t.Fatalf("export failed without a claude projects directory: %v", err)
	}
	var isolatedOutput controllerEvidenceOutput
	if err := json.Unmarshal(isolated.Bytes(), &isolatedOutput); err != nil {
		t.Fatal(err)
	}
	if isolatedOutput.Export == nil || isolatedOutput.Export.LiveSection != "collected" {
		t.Fatalf("isolated-home export result = %#v", isolatedOutput.Export)
	}
	missingTranscript := false
	for _, entry := range verifyExportArchive(t, isolatedOutput.Export) {
		if entry.Path == "live/transcripts/claude/session-export-worker" && entry.Missing {
			missingTranscript = true
		}
	}
	if !missingTranscript {
		t.Fatal("isolated-home export did not record the bound session transcript as missing")
	}
}

func archiveFileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func verifyExportArchive(t *testing.T, exported *controllerEvidenceExportOutput) []exportManifestEntry {
	t.Helper()
	archive, err := zip.OpenReader(exported.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	contents := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		_ = reader.Close()
		contents[file.Name] = data
	}
	manifestData, ok := contents["manifest.json"]
	if !ok {
		t.Fatal("export archive has no manifest")
	}
	sum := sha256.Sum256(manifestData)
	if hex.EncodeToString(sum[:]) != exported.ManifestDigest {
		t.Fatal("manifest digest does not match archive manifest bytes")
	}
	var manifest struct {
		Entries []exportManifestEntry `json:"entries"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("export manifest lists no entries")
	}
	for _, entry := range manifest.Entries {
		if entry.Missing || entry.Unreadable != "" {
			if _, ok := contents[entry.Path]; ok {
				t.Fatalf("manifest-only entry %s has archive bytes", entry.Path)
			}
			continue
		}
		data, ok := contents[entry.Path]
		if !ok {
			t.Fatalf("manifest entry %s is absent from the archive", entry.Path)
		}
		entrySum := sha256.Sum256(data)
		if hex.EncodeToString(entrySum[:]) != entry.SHA256 || int64(len(data)) != entry.Bytes {
			t.Fatalf("manifest entry %s does not match archived bytes", entry.Path)
		}
	}
	return manifest.Entries
}

func TestControllerEvidenceExportSucceedsWhileWorkflowLockHeldAndWriterActive(t *testing.T) {
	fixture := newControllerEvidenceExportFixture(t)
	lockPath, err := controller.WorkflowLockPath(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := repolock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		file, err := os.OpenFile(fixture.runtime.ModelCallLogPath(fixture.taskID), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer func() { _ = file.Close() }()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := file.WriteString("{\"version\":3,\"call_id\":\"active\"}\n"); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	var stdout bytes.Buffer
	exportErr := runExportAction(t, fixture, `{"action":"export-task-bundle"}`, &stdout)
	writer.Wait()
	if exportErr != nil {
		t.Fatalf("export failed while workflow lock held and telemetry writer active: %v", exportErr)
	}
	var output controllerEvidenceOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Export == nil || output.Export.LiveSection != "collected" {
		t.Fatalf("concurrent export result = %#v", output.Export)
	}
	verifyExportArchive(t, output.Export)
}

func TestControllerEvidenceExportRejectsInvalidCommands(t *testing.T) {
	fixture := newControllerEvidenceExportFixture(t)
	if err := runExportAction(t, fixture, `{"action":"export-task-bundle","task_path":"a.md","attempt_id":"b"}`, io.Discard); err == nil {
		t.Fatal("export accepted conflicting target fields")
	}
	if err := runExportAction(t, fixture, `{"action":"cleanup-durability","task_path":"a.md"}`, io.Discard); err == nil {
		t.Fatal("non-export action accepted export target fields")
	}
	if err := runExportAction(t, fixture, `{"action":"export-task-bundle","task_path":"IMPLEMENTATION_TASKS/missing.md"}`, io.Discard); err == nil {
		t.Fatal("export accepted an unknown task path")
	}
}
