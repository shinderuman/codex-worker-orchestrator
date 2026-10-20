package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func stubAcceptedCandidateValidation(t *testing.T, request controller.FinalValidationRequest, lookup controller.CandidateValidationLookup, err error) {
	t.Helper()
	previous := acceptedCandidateValidationRequest
	acceptedCandidateValidationRequest = func(config.AppConfig, string) (controller.FinalValidationRequest, controller.CandidateValidationLookup, error) {
		return request, lookup, err
	}
	t.Cleanup(func() { acceptedCandidateValidationRequest = previous })
}

func TestExecuteFinalizationCheckRoutesFromAcceptedCandidate(t *testing.T) {
	cases := []struct {
		name       string
		workingDir string
	}{
		{name: "declared module directory routes the gate", workingDir: "module"},
		{name: "declared repository root routes the gate", workingDir: "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFinalizationTestRepo(t)
			moduleDir := writeFinalizationModuleDir(t, repo)
			if tc.workingDir == "." {
				if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.invalid/root\n\ngo 1.22\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stubAcceptedCandidateValidation(t, controller.FinalValidationRequest{Form: "go-test", WorkingDir: tc.workingDir}, controller.CandidateValidationDeclared, nil)
			writeFinalizationGateWorker(t)
			expectedGateDir, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			if tc.workingDir != "." {
				expectedGateDir, err = filepath.EvalSymlinks(moduleDir)
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("EXPECTED_GATE_DIR", expectedGateDir)
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
			if result.Routing == nil || result.Routing.Basis != finalizationRoutingBasisCandidate || result.Routing.SelectedDir != expectedGateDir {
				t.Fatalf("routing = %#v", result.Routing)
			}
		})
	}
}

func TestExecuteFinalizationCheckRejectsUndeclaredCandidateForm(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	stubAcceptedCandidateValidation(t, controller.FinalValidationRequest{}, controller.CandidateValidationFormAbsent, nil)
	writeFinalizationGateMarkerWorker(t, marker)
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
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != "form_not_declared_by_accepted_candidate" {
		t.Fatalf("result = %#v", result)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("quality gate ran despite undeclared form: %v", statErr)
	}
}

func TestExecuteFinalizationCheckFailsClosedWhenCandidateLookupErrors(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	stubAcceptedCandidateValidation(t, controller.FinalValidationRequest{}, controller.CandidateValidationNone, errors.New("controller head unreadable"))
	writeFinalizationGateMarkerWorker(t, marker)
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
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != "accepted_candidate_unavailable" {
		t.Fatalf("result = %#v", result)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("quality gate ran despite candidate lookup failure: %v", statErr)
	}
}

func TestExecuteFinalizationCheckBlocksCandidateDirectoryOutsideRepository(t *testing.T) {
	cases := []struct {
		name       string
		workingDir string
		setup      func(t *testing.T, repo string)
		wantReason string
	}{
		{
			name:       "lexically outside declaration",
			workingDir: filepath.Join("..", "outside-module"),
			wantReason: "candidate_working_directory_outside_repository",
		},
		{
			name:       "missing directory",
			workingDir: "missing-module",
			wantReason: "candidate_working_directory_unavailable",
		},
		{
			name:       "directory without module root",
			workingDir: "plain",
			setup: func(t *testing.T, repo string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(repo, "plain"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantReason: "candidate_working_directory_not_a_module_root",
		},
		{
			name:       "symlink escape",
			workingDir: "linked",
			setup: func(t *testing.T, repo string) {
				t.Helper()
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "go.mod"), []byte("module example.invalid/outside\n\ngo 1.22\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				resolvedOutside, err := filepath.EvalSymlinks(outside)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(resolvedOutside, filepath.Join(repo, "linked")); err != nil {
					t.Fatal(err)
				}
			},
			wantReason: "candidate_working_directory_outside_repository",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFinalizationTestRepo(t)
			if tc.setup != nil {
				tc.setup(t, repo)
			}
			marker := filepath.Join(t.TempDir(), "quality-gate-called")
			stubAcceptedCandidateValidation(t, controller.FinalValidationRequest{Form: "go-test", WorkingDir: tc.workingDir}, controller.CandidateValidationDeclared, nil)
			writeFinalizationGateMarkerWorker(t, marker)
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
			if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "routing" || result.Failure.Reason != tc.wantReason {
				t.Fatalf("result = %#v", result)
			}
			if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
				t.Fatalf("quality gate ran despite routing rejection: %v", statErr)
			}
		})
	}
}

func TestExecuteFinalizationCheckCallerModuleRootWithoutCandidate(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	moduleDir := writeFinalizationModuleDir(t, repo)
	invocations := filepath.Join(t.TempDir(), "worker-invocations.log")
	workerDir := t.TempDir()
	worker := filepath.Join(workerDir, "glm-worker")
	body := `
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_GATE_DIR"
    printf 'gate\n' >>"$INVOCATION_LOG"
    printf '%s\n' '{"status":"pass","validation_run_id":"fresh-run","form":"go-test","command":"go test ./...","working_dir":"current","duration_ms":1,"head":"head-1","index_digest":"index-1","worktree_digest":"worktree-1","log":"gate.log"}'
    ;;
  --handoff)
    printf 'handoff\n' >>"$INVOCATION_LOG"
    exit 9
    ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(worker, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", workerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("INVOCATION_LOG", invocations)
	resolvedModuleDir, err := filepath.EvalSymlinks(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXPECTED_GATE_DIR", resolvedModuleDir)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(moduleDir); err != nil {
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
	if result.Routing == nil || result.Routing.Basis != finalizationRoutingBasisCaller || result.Routing.SelectedDir != resolvedModuleDir {
		t.Fatalf("routing = %#v", result.Routing)
	}
	logData, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(logData)); got != "gate" {
		t.Fatalf("worker invocation order = %q", got)
	}
}

func TestExecuteFinalizationCheckBlocksWhenNoWorkingDirectoryIsAModuleRoot(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	marker := filepath.Join(t.TempDir(), "quality-gate-called")
	stubAcceptedCandidateValidation(t, controller.FinalValidationRequest{}, controller.CandidateValidationNone, nil)
	writeFinalizationGateMarkerWorker(t, marker)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
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

func TestFinalizationVerifiedCandidateDirRejectsLexicallyOutsideDeclaration(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	selected, failure := finalizationVerifiedCandidateDir(repo, filepath.Join("..", "missing-module"))
	if selected != "" {
		t.Fatalf("outside candidate declaration selected = %q", selected)
	}
	if failure == nil || failure.Stage != "routing" || failure.Reason != "candidate_working_directory_outside_repository" {
		t.Fatalf("outside missing declaration did not fail closed: %#v", failure)
	}
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

func writeFinalizationGateWorker(t *testing.T) {
	t.Helper()
	body := `
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_GATE_DIR"
    test "$2" = "go-test"
    printf '%s\n' '{"status":"pass","validation_run_id":"fresh-run","form":"go-test","command":"go test ./...","working_dir":"current","duration_ms":1,"head":"head-1","index_digest":"index-1","worktree_digest":"worktree-1","log":"gate.log"}'
    ;;
  *) exit 9 ;;
esac
`
	activateFinalizationWorker(t, body)
}

func writeFinalizationGateMarkerWorker(t *testing.T, marker string) {
	t.Helper()
	body := `
case "$1" in
  --quality-gate)
    touch "$QUALITY_GATE_MARKER"
    exit 9
    ;;
  *) exit 9 ;;
esac
`
	t.Setenv("QUALITY_GATE_MARKER", marker)
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
