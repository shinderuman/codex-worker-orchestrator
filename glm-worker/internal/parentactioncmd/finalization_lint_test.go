package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
)

func TestMain(m *testing.M) {
	runFinalizationRepositoryLint = func(string, bool) (harnesslint.Report, error) {
		return harnesslint.Report{Status: finalizationValidationStatusPass, Violations: []harnesslint.Violation{}}, nil
	}
	os.Exit(m.Run())
}

func TestFinalizationCheckBlocksWhenRepositoryLintFails(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"status":"pass","validation_run_id":"run-lint-fail","form":"go-test"}'
    ;;
  *) exit 9 ;;
esac
`)
	setFinalizationRepositoryLint(t, func(root string, fix bool) (harnesslint.Report, error) {
		if root != repo || fix {
			t.Fatalf("lint root=%q fix=%v", root, fix)
		}
		return harnesslint.Report{
			Status: "fail",
			Violations: []harnesslint.Violation{{
				Rule: "revive", Path: "glm-worker/internal/example.go", Line: 7, Column: 2, Message: "fixture violation",
			}},
		}, nil
	})
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, repo, "go-test", nil, &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "repository_lint" || result.Failure.Reason != "repository_lint_failed" {
		t.Fatalf("result=%#v", result)
	}
	if len(result.Validation) != 0 || len(result.RepositoryLint) == 0 {
		t.Fatalf("evidence=%#v", result)
	}
}

func TestFinalizationCallerLintRejectsBeforeFullSuite(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	moduleDir := filepath.Join(repo, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	qualityMarker := filepath.Join(t.TempDir(), "quality-called")
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
	t.Setenv("QUALITY_MARKER", qualityMarker)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_VALIDATION_DIR"
    touch "$QUALITY_MARKER"
    printf '%s\n' '{"status":"pass","validation_run_id":"run-root","form":"go-test"}'
    ;;
esac
`)
	lintCalled := false
	setFinalizationRepositoryLint(t, func(root string, fix bool) (harnesslint.Report, error) {
		if root != repo || fix {
			t.Fatalf("lint root=%q fix=%v", root, fix)
		}
		if _, err := os.Stat(qualityMarker); err == nil {
			t.Fatal("caller lint ran after the full suite")
		}
		lintCalled = true
		return harnesslint.Report{Status: finalizationValidationStatusPass, Violations: []harnesslint.Violation{}}, nil
	})
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, moduleDir, "go-test", nil, &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !lintCalled || result.Status != "ready_for_parent_decision" || len(result.RepositoryLint) == 0 {
		t.Fatalf("lintCalled=%v result=%#v", lintCalled, result)
	}
}

func setFinalizationRepositoryLint(t *testing.T, run func(string, bool) (harnesslint.Report, error)) {
	t.Helper()
	previous := runFinalizationRepositoryLint
	runFinalizationRepositoryLint = run
	t.Cleanup(func() { runFinalizationRepositoryLint = previous })
}

func TestFinalizationAcceptedCandidateUsesCompletionLintOwner(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate) printf '%s\n' '{"status":"pass","validation_run_id":"run-candidate","form":"go-test"}';;
  *) exit 9;;
esac
`)
	setFinalizationRepositoryLint(t, func(string, bool) (harnesslint.Report, error) {
		t.Fatal("accepted candidate reran repository lint before its exact-commit completion owner")
		return harnesslint.Report{}, nil
	})
	var output bytes.Buffer
	routing := &finalizationRoutingSummary{SelectedDir: repo, Basis: finalizationRoutingBasisCandidate}
	if err := runFinalizationCheckWithWorker(worker, repo, repo, "go-test", routing, &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready_for_parent_decision" || len(result.Validation) == 0 || len(result.RepositoryLint) != 0 {
		t.Fatalf("candidate finalization lost validation or claimed a duplicate lint: %#v", result)
	}
}
