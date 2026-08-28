package harnesslint

import (
	"strings"
	"testing"
)

type versionRunner struct {
	outputs map[string]string
}

func (r versionRunner) run(_ string, name string, _ ...string) (commandResult, error) {
	return commandResult{output: r.outputs[name]}, nil
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
