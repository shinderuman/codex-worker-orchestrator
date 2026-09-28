package harnesslint

import "testing"

type platformLintCall struct {
	name string
	env  []string
	args []string
}

type platformLintFixtureRunner struct {
	calls        []platformLintCall
	nativeResult commandResult
	linuxResult  commandResult
	darwinResult commandResult
}

func (r *platformLintFixtureRunner) run(_ string, name string, args ...string) (commandResult, error) {
	r.calls = append(r.calls, platformLintCall{name: name, args: append([]string(nil), args...)})
	if name == "golangci-lint" {
		return r.nativeResult, nil
	}
	return commandResult{}, nil
}

func (r *platformLintFixtureRunner) runEnv(_ string, name string, env []string, args ...string) (commandResult, error) {
	r.calls = append(r.calls, platformLintCall{
		name: name,
		env:  append([]string(nil), env...),
		args: append([]string(nil), args...),
	})
	if name != "golangci-lint" {
		return commandResult{}, nil
	}
	if containsArgument(env, "GOOS=linux") && containsArgument(args, "--disable") {
		return r.linuxResult, nil
	}
	if containsArgument(env, "GOOS=darwin") && containsArgument(args, "--disable") {
		return r.darwinResult, nil
	}
	return commandResult{}, nil
}

func TestRunExternalChecksIncludesLinuxCoverageForDarwinHost(t *testing.T) {
	runner := &platformLintFixtureRunner{linuxResult: commandResult{
		output:   "internal/linux_only.go:7:12: parameter 'writeRoot' seems to be unused (revive)\n",
		exitCode: 1,
	}}
	lintRunner := crossPlatformGolangCIRunner{commandRunner: runner, hostGOOS: "darwin"}
	violations, err := runExternalChecks(t.TempDir(), []string{"glm-worker/go.mod"}, lintRunner)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations=%+v", violations)
	}
	violation := violations[0]
	if violation.Rule != "revive" || violation.Path != "glm-worker/internal/linux_only.go" || violation.Line != 7 {
		t.Fatalf("violation=%+v", violation)
	}
	if !hasPlatformLintCall(runner.calls, "GOOS=linux") {
		t.Fatal("darwin host must run linux golangci-lint coverage")
	}
	if hasPlatformLintCall(runner.calls, "GOOS=darwin") {
		t.Fatal("darwin host must not duplicate native golangci-lint coverage")
	}
}

func TestCrossPlatformGolangCIRunnerPreservesNativeLinuxFailure(t *testing.T) {
	runner := &platformLintFixtureRunner{nativeResult: commandResult{
		output:   "internal/linux_only.go:7:12: native linux failure (revive)\n",
		exitCode: 1,
	}}
	lintRunner := crossPlatformGolangCIRunner{commandRunner: runner, hostGOOS: "linux"}
	result, err := lintRunner.run(t.TempDir(), "golangci-lint", "run")
	if err != nil {
		t.Fatal(err)
	}
	if result.exitCode != 1 || result.output != runner.nativeResult.output {
		t.Fatalf("result=%+v", result)
	}
	if hasAnyCrossPlatformLintCall(runner.calls) {
		t.Fatal("native lint failure must remain authoritative before cross-platform passes")
	}
}

func hasPlatformLintCall(calls []platformLintCall, target string) bool {
	for _, call := range calls {
		if call.name == "golangci-lint" && containsArgument(call.env, target) {
			return true
		}
	}
	return false
}

func hasAnyCrossPlatformLintCall(calls []platformLintCall) bool {
	for _, call := range calls {
		if call.name == "golangci-lint" && len(call.env) > 0 {
			return true
		}
	}
	return false
}

func containsArgument(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
