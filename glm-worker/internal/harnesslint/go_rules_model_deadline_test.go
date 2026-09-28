package harnesslint

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestModelInvocationDeadlineRejectsRawProcessExecution(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/runner/raw.go"
	writeFixture(t, root, path, `package runner

func runRawModel(r *ClaudeRunner, args []string) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return command.Run()
}

func runInlineRawModel(r *ClaudeRunner, args []string) error {
	return newProcessGroupCmd(r.config.ClaudeBin, args...).Run()
}

func runRawModelDespiteTimeoutConstant(r *ClaudeRunner, args []string) error {
	timeoutSeconds := 120
	_ = timeoutSeconds
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return command.Run()
}
`)
	requireRulePath(t, ruleViolations(t, root), modelDeadlineRule, path)
}

func TestModelInvocationDeadlineRejectsZeroDeadlineProbe(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/runner/zero_deadline.go"
	writeFixture(t, root, path, `package runner

import "time"

func probeModelWithoutDeadline(r *ClaudeRunner, args []string) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return r.runProbeCommand(command, time.Time{})
}
`)
	requireRulePath(t, ruleViolations(t, root), modelDeadlineRule, path)
}

func TestModelInvocationDeadlineRejectsZeroDeadlineDelegation(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/runner/delegate_zero.go"
	writeFixture(t, root, path, `package runner

import "time"

func probeModelWithoutDeadline(r *ClaudeRunner, model string) (ProbeResult, error) {
	return r.ProbeWithDeadline(model, time.Time{})
}
`)
	requireRulePath(t, ruleViolations(t, root), modelDeadlineRule, path)
}

func TestModelInvocationDeadlineRejectsCommandWithoutBoundedSink(t *testing.T) {
	root := fixtureRoot(t)
	path := "glm-worker/internal/runner/prepare_only.go"
	writeFixture(t, root, path, `package runner

func prepareModelCommand(r *ClaudeRunner, args []string) *exec.Cmd {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	command.Dir = r.config.RepoRoot
	return command
}
`)
	requireRulePath(t, ruleViolations(t, root), modelDeadlineRule, path)
}

func TestModelInvocationDeadlineAllowsBoundedExecutionSinks(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "stop authority sink",
			source: `package runner

func runModel(r *ClaudeRunner, args []string) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return r.runCommand(command)
}
`,
		},
		{
			name: "deadline parameter sink",
			source: `package runner

import "time"

func probeModel(r *ClaudeRunner, args []string, deadline time.Time) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return r.runProbeCommand(command, deadline)
}
`,
		},
		{
			name: "probe api delegation with deadline",
			source: `package runner

import "time"

func probeModelDeadline(r *ClaudeRunner, model string, deadline time.Time) (ProbeResult, error) {
	return r.ProbeWithDeadline(model, deadline)
}
`,
		},
		{
			name: "computed deadline sink",
			source: `package runner

import "time"

func decideModel(r *ClaudeRunner, args []string) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return closeProbeOutputs(r.runProbeCommand(command, time.Now().Add(r.decisionTimeout)), nil, nil)
}
`,
		},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := fixtureRoot(t)
			path := fmt.Sprintf("glm-worker/internal/runner/bounded%d.go", index)
			writeFixture(t, root, path, testCase.source)
			for _, violation := range ruleViolations(t, root) {
				if violation.Rule == modelDeadlineRule && violation.Path == path {
					t.Fatalf("bounded model execution must not be rejected: %+v", violation)
				}
			}
		})
	}
}

func TestModelInvocationDeadlineIgnoresNonModelAndOutOfScopeExecution(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/runner/git.go", `package runner

func runGit(gitBin string, args []string) error {
	command := newProcessGroupCmd(gitBin, args...)
	return command.Run()
}
`)
	writeFixture(t, root, "glm-worker/internal/observationexec/raw_model.go", `package observationexec

func runModelRaw(claudeBin string, args []string) error {
	command := newProcessGroupCmd(claudeBin, args...)
	return command.Run()
}
`)
	writeFixture(t, root, "glm-worker/internal/runner/raw_model_test.go", `package runner

func rawModelProbe(r *ClaudeRunner, args []string) error {
	command := newProcessGroupCmd(r.config.ClaudeBin, args...)
	return command.Run()
}
`)
	for _, violation := range ruleViolations(t, root) {
		if violation.Rule == modelDeadlineRule {
			t.Fatalf("non-model or out-of-scope execution must not be flagged: %+v", violation)
		}
	}
}

func TestModelInvocationDeadlineCurrentRunnerSourcePasses(t *testing.T) {
	root := harnesslintRepositoryRoot(t)
	paths, err := repositoryPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, modelDeadlineScopePrefix) || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			t.Fatal(err)
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, violation := range modelDeadlineViolations(set, file, path, data) {
			t.Fatalf("current runner source must pass the model invocation deadline gate: %+v", violation)
		}
	}
}
