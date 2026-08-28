package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type qualityToolVersions struct {
	Go           string
	LintGo       string
	GolangCILint string
	Shellcheck   string
	Shfmt        string
}

var semanticVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

func newRealCommandRunner(root string) (realCommandRunner, error) {
	versions, err := loadQualityToolVersions(root)
	if err != nil {
		return realCommandRunner{}, err
	}
	cacheRoot := os.TempDir()
	runner := realCommandRunner{
		goToolchain:       "go" + versions.Go,
		lintGoToolchain:   "go" + versions.LintGo,
		goCache:           filepath.Join(cacheRoot, "codex-worker-orchestrator", "go-"+versions.LintGo),
		golangciLintCache: filepath.Join(cacheRoot, "codex-worker-orchestrator", "golangci-lint-"+versions.GolangCILint+"-go-"+versions.LintGo),
	}
	if err := validateQualityToolVersions(root, versions, runner); err != nil {
		return realCommandRunner{}, err
	}
	return runner, nil
}

func loadQualityToolVersions(root string) (qualityToolVersions, error) {
	data, err := os.ReadFile(filepath.Join(root, "quality-tools.yml"))
	if err != nil {
		return qualityToolVersions{}, fmt.Errorf("read quality tool contract: %w", err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || key == "" || value == "" {
			return qualityToolVersions{}, fmt.Errorf("invalid quality tool contract entry: %q", line)
		}
		values[key] = value
	}
	versions := qualityToolVersions{
		Go:           values["go"],
		LintGo:       values["lint-go"],
		GolangCILint: values["golangci-lint"],
		Shellcheck:   values["shellcheck"],
		Shfmt:        values["shfmt"],
	}
	if versions.Go == "" || versions.LintGo == "" || versions.GolangCILint == "" || versions.Shellcheck == "" || versions.Shfmt == "" {
		return qualityToolVersions{}, fmt.Errorf("quality tool contract is incomplete")
	}
	return versions, nil
}

func validateQualityToolVersions(root string, versions qualityToolVersions, runner commandRunner) error {
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
		result, err := runner.run(root, check.name, check.args...)
		if err != nil {
			return err
		}
		if result.exitCode != 0 {
			return fmt.Errorf("quality tool version command failed: %s", check.name)
		}
		got := semanticVersion.FindString(result.output)
		if got != check.want {
			return fmt.Errorf("quality tool version mismatch: %s=%s, required=%s", check.name, got, check.want)
		}
	}
	return nil
}
