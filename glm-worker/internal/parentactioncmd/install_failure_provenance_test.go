package parentactioncmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstallFailureFixture(t *testing.T, body string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	script := filepath.Join(repoRoot, "install-fixture.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, repoRoot
}

func TestRunInstallScriptRetainsExitDiagnostic(t *testing.T) {
	script, repoRoot := writeInstallFailureFixture(t, "printf '%s\\n' 'plancheck: task metadata invalid' >&2\nexit 7\n")
	var human bytes.Buffer

	output := runInstallScript(script, repoRoot, &human)

	if output.Status != installStatusFailed || output.Failure == nil {
		t.Fatalf("unexpected output: %#v", output)
	}
	if output.Failure.Stage != "install" || output.Failure.Reason != "install_script_failed" {
		t.Fatalf("unexpected failure classification: %#v", output.Failure)
	}
	if output.Failure.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", output.Failure.ExitCode)
	}
	if output.Failure.Detail != "plancheck: task metadata invalid" {
		t.Fatalf("detail = %q", output.Failure.Detail)
	}
	if !strings.Contains(human.String(), "plancheck: task metadata invalid") {
		t.Fatalf("human stderr lost child diagnostic: %q", human.String())
	}
}

func TestRunInstallScriptBoundsDiagnosticTail(t *testing.T) {
	script, repoRoot := writeInstallFailureFixture(t, "i=0\nwhile [ \"$i\" -lt 4096 ]; do\n  printf x >&2\n  i=$((i + 1))\ndone\nprintf '%s\\n' 'FINAL-INSTALL-DIAGNOSTIC' >&2\nexit 4\n")
	var human bytes.Buffer

	output := runInstallScript(script, repoRoot, &human)

	if output.Failure == nil {
		t.Fatalf("missing failure: %#v", output)
	}
	if len(output.Failure.Detail) > finalizationDiagnosticLimit {
		t.Fatalf("bounded detail length = %d, limit = %d", len(output.Failure.Detail), finalizationDiagnosticLimit)
	}
	if !strings.Contains(output.Failure.Detail, "FINAL-INSTALL-DIAGNOSTIC") {
		t.Fatalf("bounded tail lost final diagnostic: %q", output.Failure.Detail)
	}
	if human.Len() <= finalizationDiagnosticLimit {
		t.Fatalf("human output was unexpectedly bounded: %d", human.Len())
	}
}

func TestRunInstallScriptPromotesTypedChildFailure(t *testing.T) {
	script, repoRoot := writeInstallFailureFixture(t, "printf '%s\\n' '{\"status\":\"blocked\",\"failure\":{\"stage\":\"plancheck\",\"reason\":\"task_invalid\",\"detail\":\"Task contract invalid\"}}' >&2\nexit 9\n")
	var human bytes.Buffer

	output := runInstallScript(script, repoRoot, &human)

	if output.Failure == nil {
		t.Fatalf("missing failure: %#v", output)
	}
	if output.Failure.Stage != "plancheck" || output.Failure.Reason != "task_invalid" {
		t.Fatalf("typed child failure was flattened: %#v", output.Failure)
	}
	if output.Failure.Detail != "Task contract invalid" {
		t.Fatalf("typed detail = %q", output.Failure.Detail)
	}
	if output.Failure.ExitCode != 9 {
		t.Fatalf("exit code = %d, want 9", output.Failure.ExitCode)
	}
	if !strings.Contains(human.String(), `"stage":"plancheck"`) {
		t.Fatalf("human stderr lost typed child output: %q", human.String())
	}
}

func TestRunInstallScriptUnknownFailureRetainsEvidence(t *testing.T) {
	script, repoRoot := writeInstallFailureFixture(t, "exit 3\n")

	output := runInstallScript(script, repoRoot, &bytes.Buffer{})

	if output.Failure == nil {
		t.Fatalf("missing failure: %#v", output)
	}
	if output.Failure.Stage != "install" || output.Failure.Reason != "install_script_failed" {
		t.Fatalf("unexpected unknown failure classification: %#v", output.Failure)
	}
	if output.Failure.ExitCode != 3 || output.Failure.Detail == "" {
		t.Fatalf("unknown failure lost evidence: %#v", output.Failure)
	}
}

func TestRunInstallScriptSuccessStillStreamsOutput(t *testing.T) {
	script, repoRoot := writeInstallFailureFixture(t, "printf '%s\\n' 'install fixture complete'\nexit 0\n")
	var human bytes.Buffer

	output := runInstallScript(script, repoRoot, &human)

	if output.Status != installStatusInstalled || output.Failure != nil {
		t.Fatalf("unexpected success output: %#v", output)
	}
	if !strings.Contains(human.String(), "install fixture complete") {
		t.Fatalf("human output missing success stream: %q", human.String())
	}
}
