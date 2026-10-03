package harnesslint

import (
	"fmt"
	"os"
	"path/filepath"
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

func readDeterministicAutofixFixture(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "fixture.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
