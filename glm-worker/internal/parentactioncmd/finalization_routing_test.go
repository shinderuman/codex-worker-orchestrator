package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestExecuteFinalizationCheckRoutingSelection(t *testing.T) {
	cases := []struct {
		name             string
		rootGoMod        bool
		evidenceForm     string
		evidenceDir      string
		snapshotMatch    string
		wantBasis        string
		wantSnapshot     string
		wantValidationID string
		wantModuleGate   bool
	}{
		{
			name:             "exact evidence routes to validation module root",
			evidenceForm:     "go-test",
			evidenceDir:      "module",
			snapshotMatch:    "exact",
			wantBasis:        finalizationRoutingBasisValidation,
			wantSnapshot:     "exact",
			wantValidationID: "route-run",
			wantModuleGate:   true,
		},
		{
			name:             "parent metadata drift evidence routes to validation module root",
			evidenceForm:     "go-test",
			evidenceDir:      "module",
			snapshotMatch:    "parent_metadata_only",
			wantBasis:        finalizationRoutingBasisValidation,
			wantSnapshot:     "parent_metadata_only",
			wantValidationID: "route-run",
			wantModuleGate:   true,
		},
		{
			name:           "other form evidence keeps caller module root",
			rootGoMod:      true,
			evidenceForm:   "go-test-race",
			evidenceDir:    "module",
			snapshotMatch:  "exact",
			wantBasis:      finalizationRoutingBasisCaller,
			wantModuleGate: false,
		},
		{
			name:           "non module evidence directory keeps caller module root",
			rootGoMod:      true,
			evidenceForm:   "go-test",
			evidenceDir:    "plain",
			snapshotMatch:  "exact",
			wantBasis:      finalizationRoutingBasisCaller,
			wantModuleGate: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFinalizationTestRepo(t)
			plainDir := filepath.Join(repo, "plain")
			if err := os.MkdirAll(plainDir, 0o700); err != nil {
				t.Fatal(err)
			}
			moduleDir := writeFinalizationModuleDir(t, repo)
			if tc.rootGoMod {
				if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.invalid/root\n\ngo 1.22\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			evidenceDir := plainDir
			if tc.evidenceDir == "module" {
				evidenceDir = moduleDir
			}
			writeFinalizationRoutingWorker(t)
			t.Setenv("ROUTING_EVIDENCE", finalizationEvidenceJSON(evidenceDir, tc.evidenceForm, tc.snapshotMatch))
			resolvedModuleDir, err := filepath.EvalSymlinks(moduleDir)
			if err != nil {
				t.Fatal(err)
			}
			resolvedRepo, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			expectedGateDir := resolvedRepo
			if tc.wantModuleGate {
				expectedGateDir = resolvedModuleDir
			}
			t.Setenv("EXPECTED_GATE_DIR", expectedGateDir)
			t.Setenv("EXPECTED_REPO_ROOT", resolvedRepo)
			previous, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(repo); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Chdir(previous) }()

			var output bytes.Buffer
			if err := execute(config.AppConfig{RepoRoot: repo}, []string{"finalize-check", "go-test"}, &output, &output); err != nil {
				t.Fatalf("execute: %v: %s", err, output.String())
			}
			var result finalizationCheckOutput
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != "ready_for_parent_decision" || result.Failure != nil {
				t.Fatalf("result = %#v", result)
			}
			if result.Routing == nil || result.Routing.Basis != tc.wantBasis || result.Routing.SelectedDir != expectedGateDir {
				t.Fatalf("routing = %#v", result.Routing)
			}
			if result.Routing.SnapshotMatch != tc.wantSnapshot || result.Routing.ValidationRunID != tc.wantValidationID {
				t.Fatalf("routing evidence = %#v", result.Routing)
			}
		})
	}
}

func TestExecuteFinalizationCheckBlocksWhenNoWorkingDirectoryIsAModuleRoot(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	writeFinalizationRoutingWorkerWithGateMarker(t)
	t.Setenv("ROUTING_EVIDENCE", "[]")
	t.Setenv("QUALITY_GATE_MARKER", marker)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_REPO_ROOT", resolvedRepo)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	var output bytes.Buffer
	if err := execute(config.AppConfig{RepoRoot: repo}, []string{"finalize-check", "go-test"}, &output, &output); err != nil {
		t.Fatalf("execute: %v: %s", err, output.String())
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != "no_module_root_working_directory" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Failure.Detail, resolvedRepo) {
		t.Fatalf("failure detail = %#v", result.Failure)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("quality gate marker = %v", statErr)
	}
}

func TestExecuteFinalizationCheckBlocksRoutingEvidenceOutsideRepository(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "go.mod"), []byte("module example.invalid/outside\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	writeFinalizationRoutingWorkerWithGateMarker(t)
	t.Setenv("ROUTING_EVIDENCE", finalizationEvidenceJSON(outside, "go-test", "exact"))
	t.Setenv("QUALITY_GATE_MARKER", marker)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_REPO_ROOT", resolvedRepo)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	var output bytes.Buffer
	if err := execute(config.AppConfig{RepoRoot: repo}, []string{"finalize-check", "go-test"}, &output, &output); err != nil {
		t.Fatalf("execute: %v: %s", err, output.String())
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != "routing_evidence_outside_repository" {
		t.Fatalf("result = %#v", result)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("quality gate marker = %v", statErr)
	}
}

func finalizationEvidenceJSON(dir, form, snapshotMatch string) string {
	return `{"validation_run_id":"route-run","form":"` + form + `","working_dir":"` + dir + `","snapshot_match":"` + snapshotMatch + `"}`
}

func writeFinalizationModuleDir(t *testing.T, repo string) string {
	t.Helper()
	moduleDir := filepath.Join(repo, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.invalid/finalization\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

func writeFinalizationRoutingWorker(t *testing.T) {
	t.Helper()
	body := `
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_GATE_DIR"
    test "$2" = "go-test"
    printf '%s\n' '{"status":"pass","validation_run_id":"fresh-run","form":"go-test","command":"go test ./...","working_dir":"current","duration_ms":1,"log":"gate.log"}'
    ;;
  --handoff)
    test "$PWD" = "$EXPECTED_REPO_ROOT"
    printf '{"consistent":true,"validations":[{"validation_run_id":"fresh-run","status":"pass"}],"routing_evidence":[%s]}\n' "$ROUTING_EVIDENCE"
    ;;
  *) exit 9 ;;
esac
`
	activateFinalizationWorker(t, body)
}

func writeFinalizationRoutingWorkerWithGateMarker(t *testing.T) {
	t.Helper()
	body := `
case "$1" in
  --quality-gate)
    touch "$QUALITY_GATE_MARKER"
    exit 9
    ;;
  --handoff)
    test "$PWD" = "$EXPECTED_REPO_ROOT"
    printf '{"consistent":true,"validations":[],"routing_evidence":[%s]}\n' "$ROUTING_EVIDENCE"
    ;;
  *) exit 9 ;;
esac
`
	activateFinalizationWorker(t, body)
}

func activateFinalizationWorker(t *testing.T, body string) {
	t.Helper()
	workerDir := t.TempDir()
	worker := filepath.Join(workerDir, "glm-worker")
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
