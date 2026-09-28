package harnesslint

import "runtime"

type golangCIPlatformConfig struct {
	goos   string
	goarch string
}

type crossPlatformGolangCIRunner struct {
	commandRunner
	hostGOOS string
}

const golangCILintToolName = "golangci-lint"

var golangCIPlatformConfigs = []golangCIPlatformConfig{
	{goos: "linux", goarch: "amd64"},
	{goos: "darwin", goarch: "arm64"},
}

func newCrossPlatformGolangCIRunner(runner commandRunner) commandRunner {
	return crossPlatformGolangCIRunner{commandRunner: runner, hostGOOS: runtime.GOOS}
}

func (r crossPlatformGolangCIRunner) run(dir, name string, args ...string) (commandResult, error) {
	native, err := r.commandRunner.run(dir, name, args...)
	if err != nil || name != golangCILintToolName || native.exitCode != 0 {
		return native, err
	}
	for _, config := range golangCIPlatformConfigs {
		if config.goos == r.hostGOOS {
			continue
		}
		result, err := runCommandWithEnvironment(r.commandRunner, dir, name, []string{
			"GOOS=" + config.goos,
			"GOARCH=" + config.goarch,
			"CGO_ENABLED=0",
		}, args...)
		if err != nil || result.exitCode != 0 {
			return result, err
		}
	}
	return native, nil
}

func (r crossPlatformGolangCIRunner) runEnv(dir, name string, env []string, args ...string) (commandResult, error) {
	return runCommandWithEnvironment(r.commandRunner, dir, name, env, args...)
}
