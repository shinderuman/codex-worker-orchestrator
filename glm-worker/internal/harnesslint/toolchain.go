package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type qualityToolVersions struct {
	Namespace     string
	DefaultBinDir string
	Go            string
	LintGo        string
	GolangCILint  string
	Shellcheck    string
	Shfmt         string
}

type QualityToolVersionMismatch struct {
	Tool     string
	Observed string
	Required string
}

type MissingToolError struct {
	Name string
}

type QualityToolCommandError struct {
	Tool string
}

type QualityToolTimeoutError struct {
	Tool string
}

type QualityToolContractError struct {
	Cause error
}

type QualityToolFailure interface {
	error
	QualityToolClassification() string
}

const (
	QualityToolEnvironmentFailure = "environment"

	QualityToolInternalFailure = "internal"
)

var semanticVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

func (e *QualityToolVersionMismatch) Error() string {
	return fmt.Sprintf("quality tool version mismatch: %s=%s, required=%s", e.Tool, e.Observed, e.Required)
}

func (*QualityToolVersionMismatch) QualityToolClassification() string {
	return QualityToolEnvironmentFailure
}

func (e *MissingToolError) Error() string {
	return "required quality tool is missing: " + e.Name
}

func (*MissingToolError) QualityToolClassification() string {
	return QualityToolEnvironmentFailure
}

func (e *QualityToolCommandError) Error() string {
	return "quality tool version command failed: " + e.Tool
}

func (*QualityToolCommandError) QualityToolClassification() string {
	return QualityToolEnvironmentFailure
}

func (e *QualityToolTimeoutError) Error() string {
	return "quality tool version command timed out: " + e.Tool
}

func (*QualityToolTimeoutError) QualityToolClassification() string {
	return QualityToolEnvironmentFailure
}

func (e *QualityToolContractError) Error() string {
	return e.Cause.Error()
}

func (e *QualityToolContractError) Unwrap() error {
	return e.Cause
}

func (*QualityToolContractError) QualityToolClassification() string {
	return QualityToolInternalFailure
}

func PreflightQualityTools(root string) error {
	_, err := newRealCommandRunner(root)
	return err
}

func newRealCommandRunner(root string) (realCommandRunner, error) {
	versions, err := loadQualityToolVersions(root)
	if err != nil {
		return realCommandRunner{}, err
	}
	binDir, err := qualityToolsBinDir(root, versions.DefaultBinDir)
	if err != nil {
		return realCommandRunner{}, err
	}
	cacheRoot := os.TempDir()
	runner := realCommandRunner{
		goToolchain:       "go" + versions.Go,
		lintGoToolchain:   "go" + versions.LintGo,
		goCache:           filepath.Join(cacheRoot, "codex-worker-orchestrator", "go-"+versions.LintGo),
		golangciLintCache: filepath.Join(cacheRoot, "codex-worker-orchestrator", "golangci-lint-"+versions.GolangCILint+"-go-"+versions.LintGo),
		golangciLintPath:  qualityToolExecutable(binDir, versions.Namespace, "golangci-lint", versions.GolangCILint),
		shellcheckPath:    qualityToolExecutable(binDir, versions.Namespace, "shellcheck", versions.Shellcheck),
		shfmtPath:         qualityToolExecutable(binDir, versions.Namespace, "shfmt", versions.Shfmt),
	}
	if err := validateQualityToolVersions(root, versions, runner); err != nil {
		return realCommandRunner{}, err
	}
	return runner, nil
}

func qualityToolsBinDir(root, defaultBinDir string) (string, error) {
	if configured := os.Getenv("QUALITY_TOOLS_BIN_DIR"); configured != "" {
		if filepath.IsAbs(configured) {
			return filepath.Clean(configured), nil
		}
		return filepath.Clean(filepath.Join(root, configured)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", &QualityToolContractError{Cause: fmt.Errorf("resolve quality tool home: %w", err)}
	}
	return filepath.Join(home, filepath.FromSlash(defaultBinDir)), nil
}

func qualityToolExecutable(binDir, namespace, name, version string) string {
	return filepath.Join(binDir, namespace+"-"+name+"-"+version)
}

func loadQualityToolVersions(root string) (qualityToolVersions, error) {
	data, err := os.ReadFile(filepath.Join(root, "quality-tools.yml"))
	if err != nil {
		return qualityToolVersions{}, &QualityToolContractError{Cause: fmt.Errorf("read quality tool contract: %w", err)}
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || key == "" || value == "" {
			return qualityToolVersions{}, &QualityToolContractError{Cause: fmt.Errorf("invalid quality tool contract entry: %q", line)}
		}
		values[key] = value
	}
	versions := qualityToolVersions{
		Namespace:     values["namespace"],
		DefaultBinDir: values["default-bin-dir"],
		Go:            values["go"],
		LintGo:        values["lint-go"],
		GolangCILint:  values["golangci-lint"],
		Shellcheck:    values["shellcheck"],
		Shfmt:         values["shfmt"],
	}
	if versions.Namespace == "" || versions.DefaultBinDir == "" || versions.Go == "" || versions.LintGo == "" || versions.GolangCILint == "" || versions.Shellcheck == "" || versions.Shfmt == "" {
		return qualityToolVersions{}, &QualityToolContractError{Cause: fmt.Errorf("quality tool contract is incomplete")}
	}
	return versions, nil
}

func validateQualityToolVersions(root string, versions qualityToolVersions, runner versionCommandRunner) error {
	checks := []struct {
		name string
		args []string
		want string
	}{
		{name: "go", args: []string{"version"}, want: versions.Go},
		{name: "lint-go", args: []string{"version"}, want: versions.LintGo},
		{name: "golangci-lint", args: []string{"version"}, want: versions.GolangCILint},
		{name: "shellcheck", args: []string{"--version"}, want: versions.Shellcheck},
		{name: "shfmt", args: []string{"--version"}, want: versions.Shfmt},
	}
	for _, check := range checks {
		result, err := runner.runVersion(root, check.name, check.args...)
		if err != nil {
			return err
		}
		if result.exitCode != 0 {
			return &QualityToolCommandError{Tool: check.name}
		}
		got := semanticVersion.FindString(result.output)
		if got != check.want {
			return &QualityToolVersionMismatch{Tool: check.name, Observed: got, Required: check.want}
		}
	}
	return nil
}
