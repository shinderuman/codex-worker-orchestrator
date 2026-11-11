package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type deterministicSequenceRunner struct {
	postimages []string
	calls      int
}

func (runner *deterministicSequenceRunner) run(dir, name string, args ...string) (commandResult, error) {
	if name != "shfmt" || len(args) != 2 || args[0] != "-w" {
		return commandResult{}, fmt.Errorf("unexpected deterministic command: %s %v", name, args)
	}
	if runner.calls >= len(runner.postimages) {
		return commandResult{}, fmt.Errorf("unexpected deterministic pass %d", runner.calls+1)
	}
	path := filepath.Join(dir, filepath.FromSlash(args[1]))
	if err := os.WriteFile(path, []byte(runner.postimages[runner.calls]), 0o644); err != nil {
		return commandResult{}, err
	}
	runner.calls++
	return commandResult{}, nil
}

func TestDeterministicAutofixCompactsPlanSpacing(t *testing.T) {
	root := fixtureRoot(t)
	path := implementationPlanPath
	original := "## NEXT（優先順）\n\n- [first](IMPLEMENTATION_TASKS/first.md)\n\n\n- [second](IMPLEMENTATION_TASKS/second.md)\n"
	writeFixture(t, root, path, original)
	report, err := runDeterministicAutofix(root, root, fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Fixed != 1 || report.DeterministicConvergence == nil ||
		report.DeterministicConvergence.State != DeterministicFixConverged {
		t.Fatalf("deterministic fix = %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(original, "\n\n\n- [second]", "\n- [second]")
	if string(data) != expected {
		t.Fatalf("unexpected deterministic Plan: %q", data)
	}
}

func TestDeterministicAutofixConvergesAfterMultiplePasses(t *testing.T) {
	root := deterministicAutofixFixture(t, "start\n")
	runner := &deterministicSequenceRunner{postimages: []string{"middle\n", "fixed\n", "fixed\n"}}

	report, err := runDeterministicAutofix(root, root, runner)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fixed != 1 || report.DeterministicConvergence == nil {
		t.Fatalf("report = %#v", report)
	}
	evidence := report.DeterministicConvergence
	if evidence.State != DeterministicFixConverged || evidence.Iterations != 3 ||
		evidence.MaxIterations != deterministicAutofixMaxIterations || !evidence.ChangesProduced {
		t.Fatalf("convergence = %#v", evidence)
	}
	if got := readDeterministicAutofixFixture(t, root); got != "fixed\n" {
		t.Fatalf("postimage = %q", got)
	}
}

func TestDeterministicAutofixCleanTargetConvergesInOnePass(t *testing.T) {
	root := deterministicAutofixFixture(t, "clean\n")
	runner := &deterministicSequenceRunner{postimages: []string{"clean\n"}}

	report, err := runDeterministicAutofix(root, root, runner)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fixed != 0 || report.DeterministicConvergence == nil {
		t.Fatalf("report = %#v", report)
	}
	evidence := report.DeterministicConvergence
	if evidence.State != DeterministicFixConverged || evidence.Iterations != 1 || evidence.ChangesProduced {
		t.Fatalf("convergence = %#v", evidence)
	}
}

func TestDeterministicAutofixRejectsRecurringPostimageCycle(t *testing.T) {
	root := deterministicAutofixFixture(t, "a\n")
	runner := &deterministicSequenceRunner{postimages: []string{"b\n", "a\n"}}

	_, err := runDeterministicAutofix(root, root, runner)
	if err == nil {
		t.Fatal("cycle unexpectedly converged")
	}
	report, ok := DeterministicAutofixFailureReport(err)
	if !ok || report.DeterministicConvergence == nil {
		t.Fatalf("failure report = %#v err=%v", report, err)
	}
	evidence := report.DeterministicConvergence
	if evidence.State != DeterministicFixCycle || evidence.Iterations != 2 || !evidence.ChangesProduced {
		t.Fatalf("cycle evidence = %#v", evidence)
	}
}

func TestDeterministicAutofixRejectsIterationBoundExhaustion(t *testing.T) {
	root := deterministicAutofixFixture(t, "initial\n")
	postimages := make([]string, deterministicAutofixMaxIterations)
	for index := range postimages {
		postimages[index] = fmt.Sprintf("state-%d\n", index+1)
	}
	runner := &deterministicSequenceRunner{postimages: postimages}

	_, err := runDeterministicAutofix(root, root, runner)
	if err == nil {
		t.Fatal("unbounded postimages unexpectedly converged")
	}
	report, ok := DeterministicAutofixFailureReport(err)
	if !ok || report.DeterministicConvergence == nil {
		t.Fatalf("failure report = %#v err=%v", report, err)
	}
	evidence := report.DeterministicConvergence
	if evidence.State != DeterministicFixBoundExhausted || evidence.Iterations != deterministicAutofixMaxIterations || !evidence.ChangesProduced {
		t.Fatalf("bound evidence = %#v", evidence)
	}
}

func deterministicAutofixFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.sh"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestControlledRunnerUsesControlPolicyWithoutTargetToolExecution(t *testing.T) {
	target := t.TempDir()
	control := t.TempDir()
	argumentLog := filepath.Join(t.TempDir(), "arguments")
	targetMarker := filepath.Join(t.TempDir(), "target-tool-executed")
	t.Setenv("CONTROLLED_ARGUMENT_LOG", argumentLog)
	t.Setenv("TARGET_TOOL_MARKER", targetMarker)
	writeFocusedValidationFile(t, control, ".golangci.yml", "control-policy-selected\n", 0o644)
	writeFocusedValidationFile(t, target, ".golangci.yml", "target-policy-must-not-be-selected\n", 0o644)
	targetTool := "#!/bin/sh\nprintf 'executed\\n' > \"$TARGET_TOOL_MARKER\"\nexit 87\n"
	writeFocusedValidationFile(t, target, "commentlint", targetTool, 0o755)
	outsideToolRoot := t.TempDir()
	writeFocusedValidationFile(t, outsideToolRoot, "golangci-lint", targetTool, 0o755)
	t.Setenv("PATH", outsideToolRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeFocusedValidationFile(t, target, "subject.go", "package subject\n\n// explanation\nfunc Subject() {}\n", 0o644)
	writeFocusedValidationFile(t, control, "golangci-lint", `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$CONTROLLED_ARGUMENT_LOG"
while [ "$#" -gt 0 ]; do
	if [ "$1" = --config ]; then
		cat "$2"
		exit 0
	fi
	shift
done
exit 86
`, 0o755)
	runner := controlledCommandRunner{
		base:       realCommandRunner{golangciLintPath: filepath.Join(control, "golangci-lint")},
		targetRoot: target, controlRoot: control,
	}
	result, err := runner.run(target, "golangci-lint", "run", "--config", filepath.Join(target, ".golangci.yml"))
	if err != nil || result.exitCode != 0 || result.output != "control-policy-selected\n" {
		t.Fatalf("controlled policy result = %#v, err = %v", result, err)
	}
	arguments, err := os.ReadFile(argumentLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(arguments) != "run\n--config\n"+filepath.Join(control, ".golangci.yml")+"\n" {
		t.Fatalf("executed tool arguments = %q", arguments)
	}
	result, err = runner.run(target, filepath.Join(target, "commentlint"))
	if err != nil || result.exitCode != 1 {
		t.Fatalf("in-process commentlint result = %#v, err = %v", result, err)
	}
	violations := parseCommentlint(result)
	if len(violations) != 1 || violations[0].Path != "subject.go" ||
		violations[0].Rule != "commentlint/comment" || violations[0].Line != 3 {
		t.Fatalf("expected subject.go comment violation, got %+v", violations)
	}
	writeFocusedValidationFile(t, target, "subject.go", "package subject\n\nfunc Subject() {}\n", 0o644)
	result, err = runner.run(target, filepath.Join(target, "commentlint"))
	if err != nil || result.exitCode != 0 || len(parseCommentlint(result)) != 0 {
		t.Fatalf("comment-free subject must pass: result = %#v, err = %v", result, err)
	}
	if _, err := os.Stat(targetMarker); !os.IsNotExist(err) {
		t.Fatalf("target-owned tool was executed: %v", err)
	}
}

func readDeterministicAutofixFixture(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "fixture.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
