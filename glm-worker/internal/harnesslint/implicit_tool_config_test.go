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

	result, err := runner.run(t.TempDir(), shellcheckToolName, "-f", "gcc", "script.sh")
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

func TestCanonicalRunnerKeepsRealShellcheckFailureDespiteImplicitConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("quality tool fixture uses a POSIX shell")
	}
	runner, available := qualityToolIntegrationRunner(t)
	if !available {
		return
	}
	fixture := t.TempDir()
	writeQualityConfigFixture(t, fixture, "audit.sh", "#!/bin/sh\necho $1\n")
	writeQualityConfigFixture(t, fixture, ".shellcheckrc", "disable=SC2086\n")
	t.Setenv("SHELLCHECK_OPTS", "--exclude=SC2086")

	result, err := runner.run(fixture, shellcheckToolName, "-f", "gcc", "audit.sh")
	if err != nil {
		t.Fatal(err)
	}
	if result.exitCode == 0 || !strings.Contains(result.output, "SC2086") {
		t.Fatalf("implicit ShellCheck config changed canonical result: exit=%d output=%q", result.exitCode, result.output)
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

func TestCanonicalRunnerIgnoresRealShfmtEditorConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("quality tool fixture uses a POSIX shell")
	}
	runner, available := qualityToolIntegrationRunner(t)
	if !available {
		return
	}
	fixture := t.TempDir()
	writeQualityConfigFixture(t, fixture, ".editorconfig", "root = true\n[*]\nindent_style = space\nindent_size = 2\n")
	writeQualityConfigFixture(t, fixture, "audit.sh", "#!/bin/sh\nif true; then\n  echo ok\nfi\n")

	result, err := runner.run(fixture, "shfmt", "-d", "audit.sh")
	if err != nil {
		t.Fatal(err)
	}
	if result.exitCode == 0 {
		t.Fatalf("implicit EditorConfig changed canonical shfmt result: output=%q", result.output)
	}
}

func qualityToolIntegrationRunner(t *testing.T) (realCommandRunner, bool) {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(workingDir, "..", "..", ".."))
	versions, err := loadQualityToolVersions(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := qualityToolsBinDir(repoRoot, versions.DefaultBinDir)
	if err != nil {
		t.Fatal(err)
	}
	shellcheckPath := qualityToolExecutable(binDir, versions.Namespace, shellcheckToolName, versions.Shellcheck)
	shfmtPath := qualityToolExecutable(binDir, versions.Namespace, "shfmt", versions.Shfmt)
	for _, path := range []string{shellcheckPath, shfmtPath} {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				t.Skip("repository quality tools are not installed in this test job")
				return realCommandRunner{}, false
			}
			t.Fatal(err)
		}
	}
	return realCommandRunner{shellcheckPath: shellcheckPath, shfmtPath: shfmtPath}, true
}

func writeQualityConfigFixture(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
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
