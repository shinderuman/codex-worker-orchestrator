package harnesslint

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type versionRunner struct {
	outputs  map[string]string
	exitCode int
}

func (r versionRunner) runVersion(_ string, name string, _ ...string) (commandResult, error) {
	return commandResult{output: r.outputs[name], exitCode: r.exitCode}, nil
}

func TestLoadQualityToolVersions(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.25.4\nlint-go: 1.22.12\ngolangci-lint: 2.7.0\nshellcheck: 0.11.0\nshfmt: 3.13.1\n")
	versions, err := loadQualityToolVersions(root)
	if err != nil {
		t.Fatal(err)
	}
	if versions.Go != "1.25.4" || versions.LintGo != "1.22.12" || versions.GolangCILint != "2.7.0" || versions.Shellcheck != "0.11.0" || versions.Shfmt != "3.13.1" {
		t.Fatalf("versions = %+v", versions)
	}
}

func TestValidateQualityToolVersionsRejectsDrift(t *testing.T) {
	versions := qualityToolVersions{Go: "1.25.4", LintGo: "1.22.12", GolangCILint: "2.7.0", Shellcheck: "0.11.0", Shfmt: "3.13.1"}
	runner := versionRunner{outputs: map[string]string{
		"go":            "go version go1.25.4 darwin/arm64",
		"lint-go":       "go version go1.22.12 darwin/arm64",
		"golangci-lint": "golangci-lint has version 2.13.1 built with go1.27.0",
		"shellcheck":    "version: 0.11.0",
		"shfmt":         "v3.13.1",
	}}
	err := validateQualityToolVersions(t.TempDir(), versions, runner)
	if err == nil || !strings.Contains(err.Error(), "golangci-lint=2.13.1, required=2.7.0") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateQualityToolVersionsFailsClosedPerTool(t *testing.T) {
	aligned := map[string]string{
		"go":            "go version go1.25.4 darwin/arm64",
		"lint-go":       "go version go1.22.12 darwin/arm64",
		"golangci-lint": "golangci-lint has version 2.7.0 built with go1.25.4",
		"shellcheck":    "version: 0.11.0",
		"shfmt":         "v3.13.1",
	}
	versions := qualityToolVersions{Go: "1.25.4", LintGo: "1.22.12", GolangCILint: "2.7.0", Shellcheck: "0.11.0", Shfmt: "3.13.1"}
	cases := []struct {
		tool     string
		output   string
		observed string
	}{
		{tool: "go", output: "go version go1.26.1 darwin/arm64", observed: "1.26.1"},
		{tool: "lint-go", output: "go version go1.23.4 darwin/arm64", observed: "1.23.4"},
		{tool: "golangci-lint", output: "golangci-lint has version 2.13.1 built with go1.27.0", observed: "2.13.1"},
		{tool: "shellcheck", output: "version: 0.10.0", observed: "0.10.0"},
		{tool: "shfmt", output: "v3.8.0", observed: "3.8.0"},
	}
	for _, testCase := range cases {
		t.Run(testCase.tool, func(t *testing.T) {
			outputs := make(map[string]string, len(aligned))
			for name, output := range aligned {
				outputs[name] = output
			}
			outputs[testCase.tool] = testCase.output
			err := validateQualityToolVersions(t.TempDir(), versions, versionRunner{outputs: outputs})
			var mismatch *QualityToolVersionMismatch
			if err == nil || !errors.As(err, &mismatch) {
				t.Fatalf("err = %v", err)
			}
			if mismatch.Tool != testCase.tool || mismatch.Observed != testCase.observed || mismatch.Required != alignedRequiredVersion(testCase.tool) {
				t.Fatalf("mismatch = %+v", mismatch)
			}
		})
	}
}

func alignedRequiredVersion(tool string) string {
	switch tool {
	case "go":
		return "1.25.4"
	case "lint-go":
		return "1.22.12"
	case "golangci-lint":
		return "2.7.0"
	case "shellcheck":
		return "0.11.0"
	default:
		return "3.13.1"
	}
}

func TestPreflightQualityToolsSharesContractAuthorityWithLint(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.25.4\nlint-go: \n")
	preflightErr := PreflightQualityTools(root)
	_, lintErr := Run(root, false)
	if preflightErr == nil || lintErr == nil || preflightErr.Error() != lintErr.Error() {
		t.Fatalf("preflight err = %v, lint err = %v", preflightErr, lintErr)
	}
}

func TestValidateQualityToolVersionsAcceptsContract(t *testing.T) {
	versions := qualityToolVersions{Go: "1.25.4", LintGo: "1.22.12", GolangCILint: "2.7.0", Shellcheck: "0.11.0", Shfmt: "3.13.1"}
	runner := versionRunner{outputs: map[string]string{
		"go":            "go version go1.25.4 darwin/arm64",
		"lint-go":       "go version go1.22.12 darwin/arm64",
		"golangci-lint": "golangci-lint has version 2.7.0 built with go1.25.4",
		"shellcheck":    "version: 0.11.0",
		"shfmt":         "v3.13.1",
	}}
	if err := validateQualityToolVersions(t.TempDir(), versions, runner); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightQualityToolsFailsClosedWithoutToolchainDownload(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.99.99\nlint-go: 1.99.98\ngolangci-lint: 99.99.99\nshellcheck: 99.99.99\nshfmt: 99.99.99\n")
	started := time.Now()
	err := PreflightQualityTools(root)
	if err == nil {
		t.Fatal("未install toolchain要求がpreflightを通過しました")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("toolchain downloadのnetwork試行が発生しました: %s", elapsed)
	}
	var commandFailure *QualityToolCommandError
	if !errors.As(err, &commandFailure) {
		t.Fatalf("未install toolchainをtyped errorで返す必要があります: %v", err)
	}
	if commandFailure.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("未install toolchain分類 = %s", commandFailure.QualityToolClassification())
	}
}

func TestPreflightQualityToolsStopsHungVersionCommandWithinBound(t *testing.T) {
	previous := versionCommandTimeout
	versionCommandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { versionCommandTimeout = previous })
	binDir := t.TempDir()
	script := filepath.Join(binDir, "go")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.25.4\nlint-go: 1.22.12\ngolangci-lint: 2.7.0\nshellcheck: 0.11.0\nshfmt: 3.13.1\n")
	started := time.Now()
	err := PreflightQualityTools(root)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	var timeoutFailure *QualityToolTimeoutError
	if !errors.As(err, &timeoutFailure) {
		t.Fatalf("timeoutをtyped errorで返す必要があります: %v", err)
	}
	if timeoutFailure.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("timeout分類 = %s", timeoutFailure.QualityToolClassification())
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("hung commandがtimeoutで有界停止しません: %s", elapsed)
	}
}

func TestCommandEnvOverridesWithoutDuplicates(t *testing.T) {
	env := commandEnv(
		[]string{"GOPROXY=https://proxy.invalid", "GOTOOLCHAIN=go1.26.1", "PATH=/bin"},
		"GOTOOLCHAIN=go1.22.12",
		"GOPROXY=off",
	)
	want := []string{"PATH=/bin", "GOTOOLCHAIN=go1.22.12", "GOPROXY=off"}
	if !slices.Equal(env, want) {
		t.Fatalf("env = %v want %v", env, want)
	}
}

func TestValidateQualityToolVersionsReturnsTypedClassifiedFailures(t *testing.T) {
	versions := qualityToolVersions{Go: "1.25.4", LintGo: "1.22.12", GolangCILint: "2.7.0", Shellcheck: "0.11.0", Shfmt: "3.13.1"}

	mismatch := versionRunner{outputs: map[string]string{"go": "go version go1.26.1 darwin/arm64"}}
	err := validateQualityToolVersions(t.TempDir(), versions, mismatch)
	var mismatchFailure *QualityToolVersionMismatch
	if err == nil || !errors.As(err, &mismatchFailure) {
		t.Fatalf("mismatch err = %v", err)
	}
	if mismatchFailure.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("version不一致分類 = %s", mismatchFailure.QualityToolClassification())
	}

	commandFailed := versionRunner{outputs: map[string]string{}, exitCode: 1}
	err = validateQualityToolVersions(t.TempDir(), versions, commandFailed)
	var commandFailure *QualityToolCommandError
	if err == nil || !errors.As(err, &commandFailure) {
		t.Fatalf("command err = %v", err)
	}
	if commandFailure.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("version command失敗分類 = %s", commandFailure.QualityToolClassification())
	}
}

func TestPreflightQualityToolsClassifiesContractFaultAsInternal(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.25.4\nlint-go: \n")
	err := PreflightQualityTools(root)
	var contract *QualityToolContractError
	if err == nil || !errors.As(err, &contract) {
		t.Fatalf("contract err = %v", err)
	}
	if contract.QualityToolClassification() != QualityToolInternalFailure {
		t.Fatalf("contract故障分類 = %s", contract.QualityToolClassification())
	}
}

func TestPreflightQualityToolsClassifiesMissingToolAsEnvironment(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	writeFixture(t, root, "quality-tools.yml", "go: 1.25.4\nlint-go: 1.22.12\ngolangci-lint: 2.7.0\nshellcheck: 0.11.0\nshfmt: 3.13.1\n")
	err := PreflightQualityTools(root)
	var missing *MissingToolError
	if err == nil || !errors.As(err, &missing) {
		t.Fatalf("missing tool err = %v", err)
	}
	if missing.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("missing tool分類 = %s", missing.QualityToolClassification())
	}
}
