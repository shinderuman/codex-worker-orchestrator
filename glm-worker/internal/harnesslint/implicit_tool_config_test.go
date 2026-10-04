package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRealCommandRunnerDisablesImplicitShellcheckConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable uses a POSIX shell")
	}
	tool := writeQualityToolProbe(t)
	t.Setenv("SHELLCHECK_OPTS", "--exclude=SC2086")
	runner := realCommandRunner{shellcheckPath: tool}

	result, err := runner.run(t.TempDir(), "shellcheck", "-f", "gcc", "script.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.output, "args=--norc -f gcc script.sh") {
		t.Fatalf("shellcheck args = %q, want explicit --norc", result.output)
	}
	if !strings.Contains(result.output, "shellcheck_opts=") || strings.Contains(result.output, "SC2086") {
		t.Fatalf("shellcheck inherited SHELLCHECK_OPTS: %q", result.output)
	}
}

func TestRealCommandRunnerDisablesImplicitShfmtEditorConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture executable uses a POSIX shell")
	}
	tool := writeQualityToolProbe(t)
	runner := realCommandRunner{shfmtPath: tool}

	result, err := runner.run(t.TempDir(), "shfmt", "-d", "script.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.output, "args=-i=0 -d script.sh") {
		t.Fatalf("shfmt args = %q, want explicit printer configuration", result.output)
	}
}

func writeQualityToolProbe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quality-tool-probe")
	content := "#!/bin/sh\nprintf 'args=%s\\n' \"$*\"\nprintf 'shellcheck_opts=%s\\n' \"${SHELLCHECK_OPTS-<unset>}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
