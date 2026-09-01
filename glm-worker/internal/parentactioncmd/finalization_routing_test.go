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

func TestExecuteFinalizationCheckRoutesFromCurrentHandoffValidation(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	moduleDir := filepath.Join(repo, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workerDir := t.TempDir()
	worker := filepath.Join(workerDir, "glm-worker")
	body := `#!/bin/sh
set -eu
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_VALIDATION_DIR"
    test "$2" = "go-test"
    printf '%s\n' '{"status":"pass","validation_run_id":"fresh-run","form":"go-test","command":"go test ./...","working_dir":"module","duration_ms":1,"log":"gate.log"}'
    ;;
  --handoff)
    test "$PWD" = "$EXPECTED_REPO_ROOT"
    printf '{"consistent":true,"validations":[{"validation_run_id":"route-run","form":"go-test","status":"pass","working_dir":"%s"},{"validation_run_id":"fresh-run","status":"pass"}]}\n' "$EXPECTED_VALIDATION_DIR"
    ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(worker, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resolvedModuleDir, err := filepath.EvalSymlinks(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_VALIDATION_DIR", resolvedModuleDir)
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
}

func TestExecuteFinalizationCheckIgnoresMismatchedHandoffFormForRouting(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	moduleDir := filepath.Join(repo, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workerDir := t.TempDir()
	worker := filepath.Join(workerDir, "glm-worker")
	body := `#!/bin/sh
set -eu
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_REPO_ROOT"
    test "$2" = "go-test"
    printf '%s\n' '{"status":"pass","validation_run_id":"fresh-run","form":"go-test","command":"go test ./...","working_dir":"repo","duration_ms":1,"log":"gate.log"}'
    ;;
  --handoff)
    printf '{"consistent":true,"validations":[{"validation_run_id":"other-form","form":"go-test-race","status":"pass","working_dir":"%s"},{"validation_run_id":"fresh-run","status":"pass"}]}\n' "$EXPECTED_VALIDATION_DIR"
    ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(worker, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	resolvedModuleDir, err := filepath.EvalSymlinks(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_VALIDATION_DIR", resolvedModuleDir)
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
}

func TestExecuteFinalizationCheckRejectsCurrentRoutingDirectoryOutsideRepository(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	outside := t.TempDir()
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	workerDir := t.TempDir()
	worker := filepath.Join(workerDir, "glm-worker")
	body := `#!/bin/sh
set -eu
case "$1" in
  --quality-gate)
    touch "$QUALITY_GATE_MARKER"
    exit 9
    ;;
  --handoff)
    printf '{"consistent":true,"validations":[{"validation_run_id":"route-run","form":"go-test","status":"pass","working_dir":"%s"}]}\n' "$OUTSIDE_VALIDATION_DIR"
    ;;
esac
`
	if err := os.WriteFile(worker, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OUTSIDE_VALIDATION_DIR", outside)
	t.Setenv("QUALITY_GATE_MARKER", marker)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()

	var output bytes.Buffer
	err = execute(config.AppConfig{RepoRoot: repo}, []string{"finalize-check", "go-test"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "inside repository") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("quality gate marker = %v", statErr)
	}
}
