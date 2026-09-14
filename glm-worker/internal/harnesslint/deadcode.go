package harnesslint

import (
	"fmt"
	"go/build"
	"path/filepath"
	"regexp"
	"strings"
)

type deadcodeBuildConfig struct {
	name   string
	goos   string
	goarch string
}

var deadcodeProductionConfigs = []deadcodeBuildConfig{
	{name: "linux/amd64", goos: "linux", goarch: "amd64"},
	{name: "darwin/arm64", goos: "darwin", goarch: "arm64"},
	{name: "windows/amd64", goos: "windows", goarch: "amd64"},
}

var deadcodeLine = regexp.MustCompile(`^(.+\.go):(\d+):(\d+): unreachable func: (.+)$`)

type environmentCommandRunner interface {
	runEnv(dir, name string, env []string, args ...string) (commandResult, error)
}

type deadcodeFindingState struct {
	violation Violation
	deadIn    map[string]bool
}

func runDeadcodeChecks(root string, paths []string, runner commandRunner) ([]Violation, error) {
	var violations []Violation
	for _, module := range moduleDirs(paths) {
		current, err := runDeadcodeModuleChecks(root, module, runner)
		if err != nil {
			return nil, err
		}
		violations = append(violations, current...)
	}
	return violations, nil
}

func runDeadcodeModuleChecks(root, module string, runner commandRunner) ([]Violation, error) {
	dir := moduleDir(root, module)
	findings := make(map[Violation]*deadcodeFindingState)
	for _, config := range deadcodeProductionConfigs {
		result, err := runCommandWithEnvironment(runner, dir, "deadcode", []string{
			"GOOS=" + config.goos,
			"GOARCH=" + config.goarch,
			"CGO_ENABLED=0",
		}, "./...")
		if err != nil {
			return nil, err
		}
		if result.exitCode != 0 {
			return []Violation{{
				Rule: "deadcode", Path: modulePath(module), Line: 1, Column: 1,
				Message: fmt.Sprintf("deadcode %s failed: %s", config.name, compactOutput(result.output, "deadcode failed")),
			}}, nil
		}
		current, err := parseDeadcodeOutput(result.output, module, dir)
		if err != nil {
			return nil, fmt.Errorf("parse deadcode %s output: %w", config.name, err)
		}
		for _, violation := range current {
			state := findings[violation]
			if state == nil {
				state = &deadcodeFindingState{violation: violation, deadIn: make(map[string]bool)}
				findings[violation] = state
			}
			state.deadIn[config.name] = true
		}
	}

	violations := make([]Violation, 0, len(findings))
	for _, state := range findings {
		deadEverywhere, err := deadcodeFindingDeadInEveryApplicableConfig(dir, module, state)
		if err != nil {
			return nil, err
		}
		if deadEverywhere {
			violations = append(violations, state.violation)
		}
	}
	return violations, nil
}

func runCommandWithEnvironment(runner commandRunner, dir, name string, env []string, args ...string) (commandResult, error) {
	if environmentRunner, ok := runner.(environmentCommandRunner); ok {
		return environmentRunner.runEnv(dir, name, env, args...)
	}
	return runner.run(dir, name, args...)
}

func parseDeadcodeOutput(output, module, moduleRoot string) ([]Violation, error) {
	var violations []Violation
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		match := deadcodeLine.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("unrecognized output: %s", line)
		}
		path, err := normalizeDeadcodePath(moduleRoot, module, match[1])
		if err != nil {
			return nil, err
		}
		violations = append(violations, Violation{
			Rule: "deadcode", Path: path, Line: atoi(match[2]), Column: atoi(match[3]),
			Message: "unreachable production function: " + match[4],
		})
	}
	return violations, nil
}

func normalizeDeadcodePath(moduleRoot, module, rawPath string) (string, error) {
	path := filepath.Clean(rawPath)
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return "", fmt.Errorf("relativize deadcode path %q: %w", rawPath, err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("deadcode path escapes module: %q", rawPath)
		}
		path = relative
	}
	path = filepath.ToSlash(path)
	if module != "" && !strings.HasPrefix(path, module+"/") {
		path = filepath.ToSlash(filepath.Join(module, filepath.FromSlash(path)))
	}
	return path, nil
}

func deadcodeFindingDeadInEveryApplicableConfig(moduleRoot, module string, state *deadcodeFindingState) (bool, error) {
	relativePath := state.violation.Path
	if module != "" {
		prefix := module + "/"
		if !strings.HasPrefix(relativePath, prefix) {
			return false, fmt.Errorf("deadcode finding %q is outside module %q", state.violation.Path, module)
		}
		relativePath = strings.TrimPrefix(relativePath, prefix)
	}
	absolutePath := filepath.Join(moduleRoot, filepath.FromSlash(relativePath))
	applicable := 0
	for _, config := range deadcodeProductionConfigs {
		matches, err := deadcodeFileMatchesConfig(absolutePath, config)
		if err != nil {
			return false, err
		}
		if !matches {
			if state.deadIn[config.name] {
				return false, fmt.Errorf("deadcode reported %s in excluded build configuration %s", state.violation.Path, config.name)
			}
			continue
		}
		applicable++
		if !state.deadIn[config.name] {
			return false, nil
		}
	}
	if applicable == 0 {
		return false, fmt.Errorf("deadcode finding has no supported build configuration: %s", state.violation.Path)
	}
	return true, nil
}

func deadcodeFileMatchesConfig(path string, config deadcodeBuildConfig) (bool, error) {
	context := build.Default
	context.GOOS = config.goos
	context.GOARCH = config.goarch
	context.CgoEnabled = false
	context.BuildTags = nil
	matches, err := context.MatchFile(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return false, fmt.Errorf("match deadcode file %s for %s: %w", path, config.name, err)
	}
	return matches, nil
}
