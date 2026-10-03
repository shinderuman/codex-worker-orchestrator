package harnesslint

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/commentlint"
)

const (
	deterministicAutofixMaxIterations = 8
	DeterministicFixConverged         = "converged"
	DeterministicFixCycle             = "cycle"
	DeterministicFixBoundExhausted    = "iteration-bound-exhausted"
)

type controlledCommandRunner struct {
	base        commandRunner
	targetRoot  string
	controlRoot string
}

type deterministicAutofixFailure struct {
	evidence DeterministicFixConvergence
}

func (failure *deterministicAutofixFailure) Error() string {
	return fmt.Sprintf("deterministic autofix did not converge: state=%s iterations=%d max_iterations=%d",
		failure.evidence.State, failure.evidence.Iterations, failure.evidence.MaxIterations)
}

func DeterministicAutofixFailureReport(err error) (Report, bool) {
	var failure *deterministicAutofixFailure
	if !errors.As(err, &failure) {
		return Report{}, false
	}
	report := makeReport(0, nil)
	report.Status = "fail"
	evidence := failure.evidence
	report.DeterministicConvergence = &evidence
	return report, true
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
	initial, err := snapshots(root, paths)
	if err != nil {
		return Report{}, err
	}
	key, err := deterministicSnapshotKey(initial)
	if err != nil {
		return Report{}, err
	}
	seen := map[string]struct{}{key: {}}
	current := initial
	changesProduced := false
	runner := controlledCommandRunner{base: base, targetRoot: root, controlRoot: controlRoot}

	for iteration := 1; iteration <= deterministicAutofixMaxIterations; iteration++ {
		if err := runDeterministicAutofixPass(root, paths, runner); err != nil {
			return Report{}, err
		}
		paths, err = repositoryPaths(root)
		if err != nil {
			return Report{}, err
		}
		next, err := snapshots(root, paths)
		if err != nil {
			return Report{}, err
		}
		if changedSnapshotCount(current, next) == 0 {
			report := makeReport(changedSnapshotCount(initial, next), nil)
			report.DeterministicConvergence = &DeterministicFixConvergence{
				State: DeterministicFixConverged, Iterations: iteration,
				MaxIterations: deterministicAutofixMaxIterations, ChangesProduced: changesProduced,
			}
			return report, nil
		}
		changesProduced = true
		key, err = deterministicSnapshotKey(next)
		if err != nil {
			return Report{}, err
		}
		if _, repeated := seen[key]; repeated {
			return Report{}, newDeterministicAutofixFailure(DeterministicFixCycle, iteration, changesProduced)
		}
		seen[key] = struct{}{}
		current = next
	}
	return Report{}, newDeterministicAutofixFailure(DeterministicFixBoundExhausted, deterministicAutofixMaxIterations, changesProduced)
}

func runDeterministicAutofixPass(root string, paths []string, runner commandRunner) error {
	if err := fixGoFormatting(root, paths); err != nil {
		return err
	}
	if _, err := runner.run(root, filepath.Join(root, "commentlint"), "--fix"); err != nil {
		return err
	}
	return runDeterministicShellFixes(root, paths, runner)
}

func deterministicSnapshotKey(snapshot map[string][32]byte) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newDeterministicAutofixFailure(state string, iterations int, changesProduced bool) error {
	return &deterministicAutofixFailure{evidence: DeterministicFixConvergence{
		State: state, Iterations: iterations, MaxIterations: deterministicAutofixMaxIterations,
		ChangesProduced: changesProduced,
	}}
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
	if name == golangCILintToolName {
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
