package harnesslint

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/commentlint"
)

type controlledCommandRunner struct {
	base        commandRunner
	targetRoot  string
	controlRoot string
}

type deterministicAutofixFailure struct {
	evidence DeterministicFixConvergence
}

type deterministicAutofixState struct {
	initial         map[string][32]byte
	current         map[string][32]byte
	seen            map[string]struct{}
	changesProduced bool
}

const (
	deterministicAutofixMaxIterations = 8
	DeterministicFixConverged         = "converged"
	DeterministicFixCycle             = "cycle"
	DeterministicFixBoundExhausted    = "iteration-bound-exhausted"
)

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
	report.Status = reportStatusFail
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
	state, err := newDeterministicAutofixState(initial)
	if err != nil {
		return Report{}, err
	}
	runner := controlledCommandRunner{base: base, targetRoot: root, controlRoot: controlRoot}

	for iteration := 1; iteration <= deterministicAutofixMaxIterations; iteration++ {
		nextPaths, next, iterationErr := deterministicAutofixIteration(root, paths, runner)
		if iterationErr != nil {
			return Report{}, iterationErr
		}
		paths = nextPaths
		if changedSnapshotCount(state.current, next) == 0 {
			return state.convergedReport(next, iteration), nil
		}
		if err := state.recordChangedPostimage(next, iteration); err != nil {
			return Report{}, err
		}
	}
	return Report{}, newDeterministicAutofixFailure(
		DeterministicFixBoundExhausted,
		deterministicAutofixMaxIterations,
		state.changesProduced,
	)
}

func deterministicAutofixIteration(
	root string,
	paths []string,
	runner commandRunner,
) ([]string, map[string][32]byte, error) {
	if err := runDeterministicAutofixPass(root, paths, runner); err != nil {
		return nil, nil, err
	}
	nextPaths, err := repositoryPaths(root)
	if err != nil {
		return nil, nil, err
	}
	next, err := snapshots(root, nextPaths)
	if err != nil {
		return nil, nil, err
	}
	return nextPaths, next, nil
}

func newDeterministicAutofixState(initial map[string][32]byte) (*deterministicAutofixState, error) {
	key, err := deterministicSnapshotKey(initial)
	if err != nil {
		return nil, err
	}
	return &deterministicAutofixState{
		initial: initial,
		current: initial,
		seen:    map[string]struct{}{key: {}},
	}, nil
}

func (state *deterministicAutofixState) recordChangedPostimage(next map[string][32]byte, iteration int) error {
	state.changesProduced = true
	key, err := deterministicSnapshotKey(next)
	if err != nil {
		return err
	}
	if _, repeated := state.seen[key]; repeated {
		return newDeterministicAutofixFailure(DeterministicFixCycle, iteration, state.changesProduced)
	}
	state.seen[key] = struct{}{}
	state.current = next
	return nil
}

func (state *deterministicAutofixState) convergedReport(next map[string][32]byte, iteration int) Report {
	report := makeReport(changedSnapshotCount(state.initial, next), nil)
	report.DeterministicConvergence = &DeterministicFixConvergence{
		State:           DeterministicFixConverged,
		Iterations:      iteration,
		MaxIterations:   deterministicAutofixMaxIterations,
		ChangesProduced: state.changesProduced,
	}
	return report
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
		State:           state,
		Iterations:      iterations,
		MaxIterations:   deterministicAutofixMaxIterations,
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
