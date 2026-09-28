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
	marker := filepath.Join(t.TempDir(), "handoff-called")
	t.Setenv("GLM_HANDOFF_MARKER", marker)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"status":"pass","validation_run_id":"run-lint-fail","form":"go-test"}'
    ;;
  --handoff)
    touch "$GLM_HANDOFF_MARKER"
    printf '%s\n' '{"consistent":true,"validations":[{"validation_run_id":"run-lint-fail","status":"pass"}]}'
    ;;
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
	if len(result.Validation) == 0 || len(result.RepositoryLint) == 0 || len(result.Handoff) != 0 {
		t.Fatalf("evidence=%#v", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("handoff marker=%v", err)
	}
}

func TestFinalizationCheckRunsRepositoryLintAtRepositoryRootBetweenValidationAndHandoff(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	moduleDir := filepath.Join(repo, "module")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	qualityMarker := filepath.Join(t.TempDir(), "quality-called")
	t.Setenv("EXPECTED_VALIDATION_DIR", moduleDir)
	t.Setenv("EXPECTED_REPO_ROOT", repo)
	t.Setenv("QUALITY_MARKER", qualityMarker)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    test "$PWD" = "$EXPECTED_VALIDATION_DIR"
    touch "$QUALITY_MARKER"
    printf '%s\n' '{"status":"pass","validation_run_id":"run-root","form":"go-test"}'
    ;;
  --handoff)
    test "$PWD" = "$EXPECTED_REPO_ROOT"
    printf '%s\n' '{"consistent":true,"validations":[{"validation_run_id":"run-root","status":"pass"}]}'
    ;;
esac
`)
	lintCalled := false
	setFinalizationRepositoryLint(t, func(root string, fix bool) (harnesslint.Report, error) {
		if root != repo || fix {
			t.Fatalf("lint root=%q fix=%v", root, fix)
		}
		if _, err := os.Stat(qualityMarker); err != nil {
			t.Fatalf("lint ran before Go validation: %v", err)
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

func TestFinalizationCheckRechecksSnapshotAfterRepositoryLint(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"status":"pass","validation_run_id":"run-stale","form":"go-test"}'
    ;;
  --handoff)
    printf '%s\n' '{"consistent":true,"validations":[]}'
    ;;
esac
`)
	setFinalizationRepositoryLint(t, func(root string, fix bool) (harnesslint.Report, error) {
		if root != repo || fix {
			t.Fatalf("lint root=%q fix=%v", root, fix)
		}
		return harnesslint.Report{Status: finalizationValidationStatusPass, Violations: []harnesslint.Violation{}}, nil
	})
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, repo, "go-test", nil, &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "snapshot" || result.Failure.Reason != "validation_not_current_for_snapshot" {
		t.Fatalf("result=%#v", result)
	}
	if len(result.RepositoryLint) == 0 || len(result.Handoff) == 0 {
		t.Fatalf("evidence=%#v", result)
	}
}

func setFinalizationRepositoryLint(t *testing.T, run func(string, bool) (harnesslint.Report, error)) {
	t.Helper()
	previous := runFinalizationRepositoryLint
	runFinalizationRepositoryLint = run
	t.Cleanup(func() { runFinalizationRepositoryLint = previous })
}
