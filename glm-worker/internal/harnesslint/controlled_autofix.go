package harnesslint

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/commentlint"
)

type controlledCommandRunner struct {
	base        commandRunner
	targetRoot  string
	controlRoot string
}

func RunControlled(root, controlRoot string) (Report, error) {
	runner, err := newRealCommandRunner(controlRoot)
	if err != nil {
		return Report{}, err
	}
	return runControlled(root, controlRoot, runner)
}

func RunDeterministicAutofix(root, controlRoot string) (Report, error) {
	runner, err := newRealCommandRunner(controlRoot)
	if err != nil {
		return Report{}, err
	}
	return runWithIsolatedFixes(root, func(workspace string) (Report, error) {
		return runDeterministicAutofix(workspace, controlRoot, runner)
	})
}

func runDeterministicAutofix(root, controlRoot string, base commandRunner) (Report, error) {
	if err := ValidateRoot(root); err != nil {
		return Report{}, err
	}
	paths, err := repositoryPaths(root)
	if err != nil {
		return Report{}, err
	}
	before, err := snapshots(root, paths)
	if err != nil {
		return Report{}, err
	}
	if err := fixGoFormatting(root, paths); err != nil {
		return Report{}, err
	}
	runner := controlledCommandRunner{base: base, targetRoot: root, controlRoot: controlRoot}
	if _, err := runner.run(root, filepath.Join(root, "commentlint"), "--fix"); err != nil {
		return Report{}, err
	}
	if err := runDeterministicShellFixes(root, paths, runner); err != nil {
		return Report{}, err
	}
	paths, err = repositoryPaths(root)
	if err != nil {
		return Report{}, err
	}
	after, err := snapshots(root, paths)
	if err != nil {
		return Report{}, err
	}
	return makeReport(changedSnapshotCount(before, after), nil), nil
}

func runDeterministicShellFixes(root string, paths []string, runner commandRunner) error {
	for _, path := range shellFiles(paths) {
		result, err := runner.run(root, "shfmt", "-w", path)
		if err != nil {
			return err
		}
		if result.exitCode != 0 {
			return fmt.Errorf("shfmt -w %s failed: %s", path, compactOutput(result.output, "shfmt failed"))
		}
	}
	return nil
}

func runControlled(root, controlRoot string, base commandRunner) (Report, error) {
	if err := ValidateRoot(root); err != nil {
		return Report{}, err
	}
	paths, err := repositoryPaths(root)
	if err != nil {
		return Report{}, err
	}
	violations, err := checkRules(root, paths)
	if err != nil {
		return Report{}, err
	}
	runner := controlledCommandRunner{base: base, targetRoot: root, controlRoot: controlRoot}
	external, err := runExternalChecks(root, paths, runner)
	if err != nil {
		return Report{}, err
	}
	violations = append(violations, external...)
	return makeReport(0, violations), nil
}

func (runner controlledCommandRunner) run(dir, name string, args ...string) (commandResult, error) {
	if filepath.Clean(name) == filepath.Join(filepath.Clean(runner.targetRoot), "commentlint") {
		return runner.runCommentlint(args)
	}
	if name == "golangci-lint" {
		args = controlledGolangCIArgs(args, runner.controlRoot)
	}
	return runner.base.run(dir, name, args...)
}

func (runner controlledCommandRunner) runCommentlint(args []string) (commandResult, error) {
	fix := len(args) == 1 && args[0] == "--fix"
	if len(args) > 1 || len(args) == 1 && !fix {
		return commandResult{}, fmt.Errorf("unsupported controlled commentlint arguments")
	}
	report, err := commentlint.Run(runner.targetRoot, fix)
	if err != nil {
		return commandResult{}, err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return commandResult{}, err
	}
	result := commandResult{output: string(data)}
	if commentlint.IsViolation(report) {
		result.exitCode = 1
	}
	return result, nil
}

func controlledGolangCIArgs(args []string, controlRoot string) []string {
	controlled := append([]string(nil), args...)
	for index := 0; index+1 < len(controlled); index++ {
		if controlled[index] == "--config" {
			controlled[index+1] = filepath.Join(controlRoot, ".golangci.yml")
		}
	}
	return controlled
}
