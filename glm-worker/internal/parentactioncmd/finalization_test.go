package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizationCheckReturnsCurrentValidationHandoffAndGitSummary(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"status":"pass","validation_run_id":"run-1","form":"go-test","command":"go test ./...","working_dir":"repo","duration_ms":1,"log":"gate.log"}'
    ;;
  --handoff)
    printf '%s\n' '{"consistent":true,"validations":[{"validation_run_id":"run-1","status":"pass"}]}'
    ;;
  *) exit 9 ;;
esac
`)
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, "go-test", &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready_for_parent_decision" || result.Failure != nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Git == nil || result.Git.Head == "" || result.Git.Detached || result.Git.RemoteState != "not_checked" {
		t.Fatalf("git = %#v", result.Git)
	}
	if result.Git.UntrackedChanges != 1 || result.Git.Clean {
		t.Fatalf("git = %#v", result.Git)
	}
	if len(result.Validation) == 0 || len(result.Handoff) == 0 {
		t.Fatalf("missing evidence = %#v", result)
	}
}

func TestFinalizationCheckBlocksWhenValidationIsNotCurrentForHandoffSnapshot(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"status":"pass","validation_run_id":"run-2","form":"go-test","command":"go test ./...","working_dir":"repo","duration_ms":1,"log":"gate.log"}'
    ;;
  --handoff)
    printf '%s\n' '{"consistent":true,"validations":[]}'
    ;;
  *) exit 9 ;;
esac
`)
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, "go-test", &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "snapshot" || result.Failure.Reason != "validation_not_current_for_snapshot" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFinalizationCheckStopsAfterValidationFailure(t *testing.T) {
	repo := newFinalizationTestRepo(t)
	marker := filepath.Join(t.TempDir(), "handoff-called")
	t.Setenv("GLM_HANDOFF_MARKER", marker)
	worker := writeFinalizationWorker(t, `
case "$1" in
  --quality-gate)
    printf '%s\n' '{"error":{"kind":"validation_failed"}}' >&2
    exit 7
    ;;
  --handoff)
    touch "$GLM_HANDOFF_MARKER"
    printf '%s\n' '{"consistent":true,"validations":[]}'
    ;;
esac
`)
	var output bytes.Buffer
	if err := runFinalizationCheckWithWorker(worker, repo, "go-test", &output); err != nil {
		t.Fatal(err)
	}
	var result finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "blocked" || result.Failure == nil || result.Failure.Stage != "validation" || result.Failure.ExitCode != 7 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("handoff marker = %v", err)
	}
}

func TestExecuteRejectsInvalidFinalizationForm(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	var output bytes.Buffer
	if err := execute(cfg, []string{"finalize-check", "unknown"}, &output, &output); err == nil || !strings.Contains(err.Error(), "finalize-check") {
		t.Fatalf("error = %v", err)
	}
}

func newFinalizationTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q")
	runFinalizationGit(t, repo, "config", "user.email", "finalization@example.invalid")
	runFinalizationGit(t, repo, "config", "user.name", "finalization test")
	path := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(path, []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "add", "tracked.txt")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	return repo
}

func runFinalizationGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func writeFinalizationWorker(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "glm-worker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
