package harnesslint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type commandResult struct {
	output   string
	exitCode int
}

type commandRunner interface {
	run(dir, name string, args ...string) (commandResult, error)
}

type realCommandRunner struct {
	goToolchain       string
	lintGoToolchain   string
	goCache           string
	golangciLintCache string
}

type missingToolError struct {
	name string
}

var golangCILine = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\(([^()]+)\))?$`)
var golangCILineOnly = regexp.MustCompile(`^(.+?):(\d+):\s*(.+?)(?:\s+\(([^()]+)\))?$`)
var shellcheckLine = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*[^:]+:\s*(.+?)(?:\s+\[([A-Z0-9]+)\])?$`)

func (e *missingToolError) Error() string {
	return "required quality tool is missing: " + e.name
}

func (r realCommandRunner) run(dir, name string, args ...string) (commandResult, error) {
	commandName := name
	toolchain := r.goToolchain
	switch name {
	case "lint-go":
		commandName = "go"
		toolchain = r.lintGoToolchain
	case "golangci-lint":
		toolchain = r.lintGoToolchain
	}
	command := exec.Command(commandName, args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GOTOOLCHAIN="+toolchain,
		"GOCACHE="+r.goCache,
		"GOLANGCI_LINT_CACHE="+r.golangciLintCache,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		return commandResult{output: string(output)}, nil
	}
	var notFound *exec.Error
	if errors.As(err, &notFound) {
		return commandResult{}, &missingToolError{name: name}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return commandResult{output: string(output), exitCode: exitError.ExitCode()}, nil
	}
	return commandResult{}, err
}

func runExternalChecks(root string, paths []string, runner commandRunner) ([]Violation, error) {
	var violations []Violation
	comment, err := runner.run(root, filepath.Join(root, "commentlint"))
	if err != nil {
		return nil, err
	}
	violations = append(violations, parseCommentlint(comment)...)
	config := filepath.Join(root, ".golangci.yml")
	for _, module := range moduleDirs(paths) {
		dir := moduleDir(root, module)
		baseArgs := []string{
			"run", "--config", config,
			"--max-issues-per-linter", "0", "--max-same-issues", "0",
			"--disable", "cyclop,gocognit,funlen,goconst",
		}
		result, err := runner.run(dir, "golangci-lint", baseArgs...)
		if err != nil {
			return nil, err
		}
		violations = append(violations, parseGolangCI(result, module)...)
		productionArgs := []string{
			"run", "--config", config,
			"--max-issues-per-linter", "0", "--max-same-issues", "0",
			"--tests=false", "--enable-only", "cyclop,gocognit,funlen,goconst",
		}
		result, err = runner.run(dir, "golangci-lint", productionArgs...)
		if err != nil {
			return nil, err
		}
		violations = append(violations, parseGolangCI(result, module)...)
	}
	for _, path := range shellFiles(paths) {
		result, err := runner.run(root, "shellcheck", "-f", "gcc", path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, parseShellcheck(result, path)...)
		result, err = runner.run(root, "shfmt", "-d", path)
		if err != nil {
			return nil, err
		}
		if result.exitCode != 0 {
			violations = append(violations, Violation{
				Rule: "shfmt", Path: path, Line: 1, Column: 1,
				Message: "shell formatting differs from shfmt output", Fixable: true,
			})
		}
	}
	return violations, nil
}

func runExternalFixers(root string, paths []string, runner commandRunner) error {
	if _, err := runner.run(root, filepath.Join(root, "commentlint"), "--fix"); err != nil {
		return err
	}
	for _, path := range shellFiles(paths) {
		result, err := runner.run(root, "shfmt", "-w", path)
		if err != nil {
			return err
		}
		if result.exitCode != 0 {
			return fmt.Errorf("shfmt -w %s failed: %s", path, strings.TrimSpace(result.output))
		}
	}
	config := filepath.Join(root, ".golangci.yml")
	for _, module := range moduleDirs(paths) {
		dir := moduleDir(root, module)
		baseArgs := []string{
			"run", "--fix", "--config", config,
			"--max-issues-per-linter", "0", "--max-same-issues", "0",
			"--disable", "cyclop,gocognit,funlen,goconst",
		}
		if _, err := runner.run(dir, "golangci-lint", baseArgs...); err != nil {
			return err
		}
		productionArgs := []string{
			"run", "--fix", "--config", config,
			"--max-issues-per-linter", "0", "--max-same-issues", "0",
			"--tests=false", "--enable-only", "cyclop,gocognit,funlen,goconst",
		}
		if _, err := runner.run(dir, "golangci-lint", productionArgs...); err != nil {
			return err
		}
	}
	return nil
}

func moduleDir(root, module string) string {
	if module == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(module))
}

func parseGolangCI(result commandResult, module string) []Violation {
	if result.exitCode == 0 {
		return nil
	}
	var violations []Violation
	for _, line := range strings.Split(result.output, "\n") {
		trimmed := strings.TrimSpace(line)
		match := golangCILine.FindStringSubmatch(trimmed)
		if match != nil && strings.HasSuffix(match[1], ".go") {
			violations = append(violations, golangCIViolation(module, match[1], match[2], match[3], match[4], match[5]))
			continue
		}
		lineOnly := golangCILineOnly.FindStringSubmatch(trimmed)
		if lineOnly != nil && strings.HasSuffix(lineOnly[1], ".go") {
			violations = append(violations, golangCIViolation(module, lineOnly[1], lineOnly[2], "1", lineOnly[3], lineOnly[4]))
		}
	}
	if len(violations) == 0 {
		violations = append(violations, Violation{
			Rule: "golangci-lint", Path: modulePath(module), Line: 1, Column: 1,
			Message: compactOutput(result.output, "golangci-lint failed"),
		})
	}
	return violations
}

func golangCIViolation(module, rawPath, rawLine, rawColumn, message, rule string) Violation {
	path := filepath.ToSlash(rawPath)
	if module != "" && !strings.HasPrefix(path, module+"/") {
		path = filepath.ToSlash(filepath.Join(module, path))
	}
	if rule == "" {
		rule = "golangci-lint"
	}
	return Violation{Rule: rule, Path: path, Line: atoi(rawLine), Column: atoi(rawColumn), Message: message}
}

func parseShellcheck(result commandResult, path string) []Violation {
	if result.exitCode == 0 {
		return nil
	}
	var violations []Violation
	for _, line := range strings.Split(result.output, "\n") {
		match := shellcheckLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		rule := strings.ToLower(match[5])
		if rule == "" {
			rule = "shellcheck"
		}
		violations = append(violations, Violation{
			Rule: rule, Path: filepath.ToSlash(match[1]), Line: atoi(match[2]), Column: atoi(match[3]), Message: match[4],
		})
	}
	if len(violations) == 0 {
		violations = append(violations, Violation{
			Rule: "shellcheck", Path: path, Line: 1, Column: 1, Message: compactOutput(result.output, "shellcheck failed"),
		})
	}
	return violations
}

func parseCommentlint(result commandResult) []Violation {
	var report struct {
		Status     string `json:"status"`
		Violations []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"violations"`
	}
	if json.Unmarshal([]byte(result.output), &report) == nil && report.Status != "" {
		var violations []Violation
		for _, item := range report.Violations {
			violations = append(violations, Violation{
				Rule: "commentlint/" + item.Kind, Path: item.Path, Line: item.Line, Column: item.Column,
				Message: item.Message, Fixable: item.Kind == "comment",
			})
		}
		return violations
	}
	if result.exitCode == 0 {
		return nil
	}
	return []Violation{{
		Rule: "commentlint", Path: "commentlint", Line: 1, Column: 1,
		Message: compactOutput(result.output, "commentlint failed"),
	}}
}

func modulePath(module string) string {
	if module == "" {
		return "go.mod"
	}
	return filepath.ToSlash(filepath.Join(module, "go.mod"))
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func compactOutput(output, fallback string) string {
	value := strings.TrimSpace(output)
	if value == "" {
		return fallback
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
