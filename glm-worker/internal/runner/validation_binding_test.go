package runner

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestValidationObservationsRecognizeTypecheckAndRepeatedRuns(t *testing.T) {
	got := validationObservationForms(validationObservationsForCommand("npx tsc --noEmit; go test ./...; go test ./..."))
	want := []string{"tsc", "go-test", "go-test"}
	if !slices.Equal(got, want) {
		t.Fatalf("forms = %v, want %v", got, want)
	}
}

func TestBindValidationObservationsAddsSnapshotPhaseAndRetry(t *testing.T) {
	repoRoot := t.TempDir()
	gitValidationBinding(t, repoRoot, "init", "-q")
	gitValidationBinding(t, repoRoot, "-c", "user.name=validation test", "-c", "user.email=validation@example.invalid", "commit", "--allow-empty", "-q", "-m", "seed")

	st := newTestStateStore(t)
	if err := st.Write("repo-root", repoRoot); err != nil {
		t.Fatal(err)
	}
	ingester := newStreamEventIngester(st, "11111111-1111-4111-8111-111111111111", "call-1", state.WorkerRole, "worker-new", "worker", "", false)
	values := []state.TaskValidationObservation{{Form: "go-test", GateClass: state.ValidationGateClassTest, Suite: "go-test"}}

	first := ingester.bindValidationObservations(values)
	second := ingester.bindValidationObservations(values)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("bound observations = %#v / %#v", first, second)
	}
	if first[0].SnapshotID == "" || first[0].SnapshotID != second[0].SnapshotID {
		t.Fatalf("snapshot ids = %q / %q", first[0].SnapshotID, second[0].SnapshotID)
	}
	if first[0].Phase != "worker-new" || second[0].Phase != "worker-new" {
		t.Fatalf("phases = %q / %q", first[0].Phase, second[0].Phase)
	}
	if first[0].Attempt != state.ValidationAttemptInitial || second[0].Attempt != state.ValidationAttemptRetry {
		t.Fatalf("attempts = %q / %q", first[0].Attempt, second[0].Attempt)
	}
}

func TestValidationAttemptsPersistAcrossRunnerCallsAndResetPerTask(t *testing.T) {
	st := newTestStateStore(t)
	runner := NewClaudeRunner(config.AppConfig{}, st)
	values := []state.TaskValidationObservation{{Form: "go-test", GateClass: state.ValidationGateClassTest, Suite: "go-test"}}

	firstCall := runner.newTaskEventIngester("task-a", "call-1", state.WorkerRole, "worker-new", "worker", "", false)
	secondCall := runner.newTaskEventIngester("task-a", "call-2", state.WorkerRole, "worker-explicit-fix", "worker", "", true)
	otherTask := runner.newTaskEventIngester("task-b", "call-3", state.WorkerRole, "worker-new", "worker", "", false)
	first := firstCall.bindValidationObservations(values)
	second := secondCall.bindValidationObservations(values)
	third := otherTask.bindValidationObservations(values)
	if first[0].Attempt != state.ValidationAttemptInitial || second[0].Attempt != state.ValidationAttemptRetry || third[0].Attempt != state.ValidationAttemptInitial {
		t.Fatalf("attempts across calls/tasks = %q / %q / %q", first[0].Attempt, second[0].Attempt, third[0].Attempt)
	}
}

func gitValidationBinding(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
