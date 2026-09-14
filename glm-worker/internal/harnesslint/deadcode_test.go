package harnesslint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type deadcodeFixtureRunner struct {
	outputs map[string]commandResult
	args    [][]string
}

func (r *deadcodeFixtureRunner) run(_ string, _ string, args ...string) (commandResult, error) {
	r.args = append(r.args, slices.Clone(args))
	return commandResult{}, nil
}

func (r *deadcodeFixtureRunner) runEnv(_ string, _ string, env []string, args ...string) (commandResult, error) {
	r.args = append(r.args, slices.Clone(args))
	goos := ""
	for _, value := range env {
		if strings.HasPrefix(value, "GOOS=") {
			goos = strings.TrimPrefix(value, "GOOS=")
			break
		}
	}
	return r.outputs[goos], nil
}

func TestDeadcodeProductionReachabilityIgnoresTests(t *testing.T) {
	runner := installedDeadcodeRunner(t)
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/deadcodefixture\n\ngo 1.22\n")
	writeFixture(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeFixture(t, root, "feature.go", "package main\n\nfunc newFeature() {\n\tstep1()\n}\n\nfunc step1() {}\n")
	writeFixture(t, root, "feature_test.go", "package main\n\nimport \"testing\"\n\nfunc TestFeature(t *testing.T) {\n\tnewFeature()\n}\n")

	violations, err := runDeadcodeModuleChecks(root, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDeadcodeSymbol(violations, "newFeature") || !hasDeadcodeSymbol(violations, "step1") {
		t.Fatalf("test-only production functions must be rejected: %+v", violations)
	}

	writeFixture(t, root, "main.go", "package main\n\nfunc main() {\n\tnewFeature()\n}\n")
	violations, err = runDeadcodeModuleChecks(root, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("production-reachable functions must pass: %+v", violations)
	}
}

func TestDeadcodeAggregatesOnlyApplicableBuildConfigurations(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.com/deadcodefixture\n")
	writeFixture(t, root, "common.go", "package fixture\n\nfunc commonOnly() {}\n")
	writeFixture(t, root, "unix.go", "//go:build unix\n\npackage fixture\n\nfunc unixOnly() {}\n")
	writeFixture(t, root, "linux.go", "//go:build linux\n\npackage fixture\n\nfunc linuxOnly() {}\n")

	runner := &deadcodeFixtureRunner{outputs: map[string]commandResult{
		"linux":   {output: "common.go:3:6: unreachable func: commonOnly\nunix.go:5:6: unreachable func: unixOnly\nlinux.go:5:6: unreachable func: linuxOnly\n"},
		"darwin":  {output: "unix.go:5:6: unreachable func: unixOnly\n"},
		"windows": {},
	}}
	violations, err := runDeadcodeModuleChecks(root, "", runner)
	if err != nil {
		t.Fatal(err)
	}
	if hasDeadcodeSymbol(violations, "commonOnly") {
		t.Fatalf("common function live on another supported configuration must not be reported: %+v", violations)
	}
	if !hasDeadcodeSymbol(violations, "unixOnly") {
		t.Fatalf("unix function dead on every applicable configuration must be reported: %+v", violations)
	}
	if !hasDeadcodeSymbol(violations, "linuxOnly") {
		t.Fatalf("linux-only function dead on its applicable configuration must be reported: %+v", violations)
	}
	for _, args := range runner.args {
		if !slices.Equal(args, []string{"./..."}) {
			t.Fatalf("deadcode enforcement must use production roots without -test: %v", args)
		}
	}
}

func installedDeadcodeRunner(t *testing.T) realCommandRunner {
	t.Helper()
	repoRoot := harnesslintRepositoryRoot(t)
	versions, err := loadQualityToolVersions(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := qualityToolsBinDir(repoRoot, versions.DefaultBinDir)
	if err != nil {
		t.Fatal(err)
	}
	path := qualityToolExecutable(binDir, versions.Namespace, "deadcode", versions.Deadcode)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("repository-owned deadcode is not installed: %s", path)
		}
		t.Fatal(err)
	}
	return realCommandRunner{
		goToolchain:  "local",
		goCache:      filepath.Join(t.TempDir(), "go-cache"),
		deadcodePath: path,
	}
}

func harnesslintRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func hasDeadcodeSymbol(violations []Violation, symbol string) bool {
	for _, violation := range violations {
		if violation.Rule == "deadcode" && strings.HasSuffix(violation.Message, symbol) {
			return true
		}
	}
	return false
}
