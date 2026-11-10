package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

const exportBundleWorkerJSON = `{"action":"export-task-bundle","export":{"target":{"kind":"task-id","task":{"task_path":"IMPLEMENTATION_TASKS/root.md"},"attempt_id":"0123456789abcdef","runtime_task_id":"runtime-task-1","selection_basis":"live-runtime-binding","live":true},"archive_path":"/exports/attempt.zip","manifest_digest":"digest-1","attempt_section":"absent","runtime_section":"collected","coverage":"partial","authority_changed_during_export":false,"controller_generation_before":1,"controller_generation_after":1}}`

func TestExportBundleSelectionStrictness(t *testing.T) {
	valid := map[string]struct {
		args   []string
		taskID string
	}{
		"default": {args: []string{"export-bundle"}},
		"task id": {args: []string{"export-bundle", "--task-id", "runtime-task-1"}, taskID: "runtime-task-1"},
	}
	for name, validCase := range valid {
		t.Run(name, func(t *testing.T) {
			selection, err := parseExportBundleSelection(validCase.args)
			if err != nil || selection.taskID != validCase.taskID {
				t.Fatalf("selection = %#v err = %v", selection, err)
			}
		})
	}
	for name, args := range map[string][]string{
		"duplicate task id": {"export-bundle", "--task-id", "a", "--task-id", "b"},
		"retired attempt":   {"export-bundle", "--attempt-id", "a"},
		"retired task path": {"export-bundle", "--task-path", "a"},
		"positional target": {"export-bundle", "runtime-task-1"},
		"missing value":     {"export-bundle", "--task-id"},
		"empty task id":     {"export-bundle", "--task-id", ""},
		"unknown option":    {"export-bundle", "--live", "true"},
		"unknown bare flag": {"export-bundle", "--task-id", "a", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseExportBundleSelection(args); err == nil {
				t.Fatalf("selection accepted invalid args %v", args)
			}
		})
	}
}

func TestExportBundleTransportsAuthorityRequestAndFlattensExport(t *testing.T) {
	repo := t.TempDir()
	capture := t.TempDir()
	t.Setenv("EXPORT_BUNDLE_CAPTURE", capture)
	stub := writeExportBundleWorkerStub(t, exportBundleWorkerJSON, 0)
	original := resolveExportBundleWorker
	defer func() { resolveExportBundleWorker = original }()
	resolveExportBundleWorker = func() (string, error) { return stub, nil }
	cfg := config.AppConfig{RepoRoot: repo}

	for name, args := range map[string][]string{
		"default": {"export-bundle"},
		"task id": {"export-bundle", "--task-id", "runtime-task-1"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, captureFile := range []string{"argv", "stdin"} {
				if err := os.Remove(filepath.Join(capture, captureFile)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			var stdout bytes.Buffer
			if err := execute(cfg, args, &stdout, io.Discard); err != nil {
				t.Fatal(err)
			}
			argv, err := os.ReadFile(filepath.Join(capture, "argv"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(argv)) != "--authority\ncontroller-evidence" {
				t.Fatalf("worker argv = %q", string(argv))
			}
			stdinPayload, err := os.ReadFile(filepath.Join(capture, "stdin"))
			if err != nil {
				t.Fatal(err)
			}
			var request map[string]any
			if err := json.Unmarshal(stdinPayload, &request); err != nil {
				t.Fatalf("worker stdin is not the synthesized request: %v: %s", err, string(stdinPayload))
			}
			if request["action"] != "export-task-bundle" {
				t.Fatalf("worker request = %#v", request)
			}
			if _, present := request["attempt_id"]; present {
				t.Fatalf("worker request kept retired attempt_id: %#v", request)
			}
			if _, present := request["task_path"]; present {
				t.Fatalf("worker request kept retired task_path: %#v", request)
			}
			if request["task_id"] != nil && request["task_id"] != "runtime-task-1" {
				t.Fatalf("worker request task_id = %#v", request["task_id"])
			}
			if _, present := request["task_id"]; present != (len(args) == 3 && args[1] == "--task-id") {
				t.Fatalf("worker request task_id presence = %v args = %v", present, args)
			}
			assertExportBundleFlattenedOutput(t, stdout.Bytes())
		})
	}
	if _, err := os.Stat(filepath.Join(repo, ".glm-worker-parent-actions")); !os.IsNotExist(err) {
		t.Fatal("export-bundle created parent action staging files")
	}
}

func TestExportBundleRejectsFailedOrMalformedWorkerOutput(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("EXPORT_BUNDLE_CAPTURE", t.TempDir())
	original := resolveExportBundleWorker
	defer func() { resolveExportBundleWorker = original }()

	for name, stubSpec := range map[string]struct {
		workerOutput string
		exitCode     int
	}{
		"worker failure":       {workerOutput: "{\"error\":\"boom\"}", exitCode: 1},
		"non JSON":             {workerOutput: "not-json", exitCode: 0},
		"multiple JSON values": {workerOutput: exportBundleWorkerJSON + "\n" + exportBundleWorkerJSON, exitCode: 0},
		"missing export":       {workerOutput: `{"action":"export-task-bundle"}`, exitCode: 0},
		"null export":          {workerOutput: `{"action":"export-task-bundle","export":null}`, exitCode: 0},
		"wrong action":         {workerOutput: `{"action":"build-task-bundle","export":{"archive_path":"/exports/a.zip","manifest_digest":"d","attempt_section":"present","runtime_section":"absent","coverage":"attempt-only","target":{"kind":"task-id","live":false}}}`, exitCode: 0},
		"missing archive path": {workerOutput: `{"action":"export-task-bundle","export":{"manifest_digest":"d","attempt_section":"absent","runtime_section":"collected","coverage":"partial","target":{"kind":"task-id","live":true}}}`, exitCode: 0},
		"missing coverage":     {workerOutput: `{"action":"export-task-bundle","export":{"archive_path":"/exports/a.zip","manifest_digest":"d","attempt_section":"absent","runtime_section":"collected","target":{"kind":"task-id","live":true}}}`, exitCode: 0},
		"missing target":       {workerOutput: `{"action":"export-task-bundle","export":{"archive_path":"/exports/a.zip","manifest_digest":"d","attempt_section":"absent","runtime_section":"collected","coverage":"partial"}}`, exitCode: 0},
		"unknown worker field": {workerOutput: `{"action":"export-task-bundle","export":{"archive_path":"/exports/a.zip","manifest_digest":"d","attempt_section":"absent","runtime_section":"collected","coverage":"partial","target":{"kind":"task-id","live":true}},"bundle":{}}`, exitCode: 0},
		"empty worker output":  {workerOutput: "", exitCode: 0},
	} {
		t.Run(name, func(t *testing.T) {
			stub := writeExportBundleWorkerStub(t, stubSpec.workerOutput, stubSpec.exitCode)
			resolveExportBundleWorker = func() (string, error) { return stub, nil }
			var stdout bytes.Buffer
			if err := execute(config.AppConfig{RepoRoot: repo}, []string{"export-bundle"}, &stdout, io.Discard); err == nil {
				t.Fatalf("export-bundle accepted malformed worker output %q", stubSpec.workerOutput)
			}
			if stdout.Len() != 0 {
				t.Fatalf("rejected export wrote partial stdout: %s", stdout.String())
			}
		})
	}
}

func writeExportBundleWorkerStub(t *testing.T, stdoutPayload string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "glm-worker")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$EXPORT_BUNDLE_CAPTURE/argv\"\n" +
		"cat > \"$EXPORT_BUNDLE_CAPTURE/stdin\"\n" +
		"printf '%s' " + shellQuote(stdoutPayload) + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := execfile.Write(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertExportBundleFlattenedOutput(t *testing.T, raw []byte) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var output map[string]any
	if err := decoder.Decode(&output); err != nil {
		t.Fatalf("export stdout is not machine JSON: %v: %s", err, string(raw))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		t.Fatalf("export stdout contains multiple JSON values: %s", string(raw))
	}
	if output["action"] != "export-task-bundle" {
		t.Fatalf("export action = %#v", output["action"])
	}
	if _, nested := output["export"]; nested {
		t.Fatalf("export stdout kept the nested export envelope: %s", string(raw))
	}
	for _, field := range []string{"archive_path", "manifest_digest", "attempt_section", "runtime_section", "coverage", "authority_changed_during_export", "controller_generation_before", "controller_generation_after"} {
		if _, present := output[field]; !present {
			t.Fatalf("export stdout is missing top-level %s: %s", field, string(raw))
		}
	}
	if output["archive_path"] != "/exports/attempt.zip" || output["coverage"] != "partial" {
		t.Fatalf("export summary = %#v", output)
	}
	var source parentExportBundleWorkerOutput
	if err := json.Unmarshal([]byte(exportBundleWorkerJSON), &source); err != nil {
		t.Fatal(err)
	}
	if !equalJSONValues(t, source.Export.Target, output["target"]) {
		t.Fatalf("export target was not preserved: %#v", output["target"])
	}
}

func equalJSONValues(t *testing.T, raw json.RawMessage, value any) bool {
	t.Helper()
	var expected any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(expected, value)
}

func TestExportBundleSelectsInstalledWorkerOverStalePATH(t *testing.T) {
	capture := t.TempDir()
	t.Setenv("EXPORT_BUNDLE_CAPTURE", capture)
	stale := writeExportBundleWorkerStub(t, "stale worker", 0)
	t.Setenv("PATH", filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))

	binDir := t.TempDir()
	t.Setenv("GLM_WORKER_BIN_DIR", binDir)
	current := writeExportBundleWorkerStub(t, exportBundleWorkerJSON, 0)
	payload, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(binDir, "glm-worker")
	if err := os.WriteFile(installed, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveInstalledExportWorker()
	if err != nil || resolved != installed {
		t.Fatalf("export worker resolved to %q, error=%v; expected installed %q", resolved, err, installed)
	}
	direct, err := resolveControllerActionWorker(config.AppConfig{}, "controller-evidence", exportBundleAction, nil)
	if err != nil || direct != installed {
		t.Fatalf("direct controller evidence resolved to %q, error=%v; expected installed %q", direct, err, installed)
	}
	var output bytes.Buffer
	if err := executeExportBundleAction(config.AppConfig{RepoRoot: t.TempDir()}, []string{"export-bundle"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertExportBundleFlattenedOutput(t, output.Bytes())

	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stale, installed); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveInstalledExportWorker(); err == nil {
		t.Fatal("symlinked installed export worker was accepted")
	}
	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveInstalledExportWorker(); err == nil {
		t.Fatal("non-executable installed export worker was accepted")
	}
}

func TestExportBundleMissingExplicitInstalledWorkerFailsClosed(t *testing.T) {
	stale := writeExportBundleWorkerStub(t, exportBundleWorkerJSON, 0)
	t.Setenv("PATH", filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))
	capture := t.TempDir()
	t.Setenv("EXPORT_BUNDLE_CAPTURE", capture)

	for _, tc := range []struct {
		name string
		dir  string
	}{
		{name: "missing worker file", dir: t.TempDir()},
		{name: "missing install directory", dir: filepath.Join(t.TempDir(), "missing")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GLM_WORKER_BIN_DIR", tc.dir)
			if worker, err := resolveInstalledExportWorker(); worker != "" || !os.IsNotExist(err) {
				t.Fatalf("explicit missing worker resolved to %q, err=%v", worker, err)
			}
			if worker, err := resolveControllerActionWorker(config.AppConfig{}, "controller-evidence", exportBundleAction, nil); worker != "" || !os.IsNotExist(err) {
				t.Fatalf("direct export selected a fallback worker: %q, err=%v", worker, err)
			}
			var output bytes.Buffer
			err := executeExportBundleAction(config.AppConfig{RepoRoot: t.TempDir()}, []string{"export-bundle"}, &output, io.Discard)
			if !os.IsNotExist(err) || output.Len() != 0 {
				t.Fatalf("missing explicit worker executed fallback: err=%v, stdout=%q", err, output.String())
			}
			if _, err := os.Stat(filepath.Join(capture, "argv")); !os.IsNotExist(err) {
				t.Fatalf("stale PATH worker was invoked: %v", err)
			}
		})
	}
}

func TestExportBundleSourceOnlyFallbackUsesLegacyResolver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GLM_WORKER_BIN_DIR", "")
	stale := writeExportBundleWorkerStub(t, exportBundleWorkerJSON, 0)
	t.Setenv("PATH", filepath.Dir(stale)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EXPORT_BUNDLE_CAPTURE", t.TempDir())

	legacy, err := resolveGLMWorker()
	if err != nil {
		t.Fatal(err)
	}
	worker, err := resolveInstalledExportWorker()
	if err != nil || worker != legacy {
		t.Fatalf("source-only fallback worker=%q, err=%v, legacy=%q", worker, err, legacy)
	}
	direct, err := resolveControllerActionWorker(config.AppConfig{}, "controller-evidence", exportBundleAction, nil)
	if err != nil || direct != legacy {
		t.Fatalf("source-only direct export worker=%q, err=%v, legacy=%q", direct, err, legacy)
	}
	var output bytes.Buffer
	if err := executeExportBundleAction(config.AppConfig{RepoRoot: t.TempDir()}, []string{"export-bundle"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	assertExportBundleFlattenedOutput(t, output.Bytes())
}
