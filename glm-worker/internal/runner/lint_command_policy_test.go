package runner

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildRunArgsDisallowsMutableWorkerDirectLintCommands(t *testing.T) {
	runner := &ClaudeRunner{}
	args := runner.buildRunArgs(
		state.WorkerRole,
		"task",
		"session",
		false,
		"model",
		false,
		"high",
		"prompt",
		runInputs{systemFile: "WORKER.md", schema: "{}"},
	)

	wantRules := []string{
		"Bash(*harnesslint*)",
		"Bash(*commentlint*)",
		"Bash(*gofmt*)",
		"Bash(*golangci-lint*)",
		"Bash(*shellcheck*)",
		"Bash(*shfmt*)",
	}
	if len(workerLintDisallowedTools) != len(wantRules) {
		t.Fatalf("worker lint deny rules = %#v", workerLintDisallowedTools)
	}
	if !containsArgument(args, "--disallowedTools") {
		t.Fatalf("worker args do not contain --disallowedTools: %#v", args)
	}
	for _, rule := range wantRules {
		if !containsArgument(args, rule) {
			t.Fatalf("worker lint deny rule %q is missing: %#v", rule, args)
		}
	}
	if containsArgument(args, "Bash") {
		t.Fatalf("mutable worker Bash tool was removed instead of narrowly denying lint commands: %#v", args)
	}
}

func TestBuildRunArgsKeepsReadOnlyBashDenyCanonical(t *testing.T) {
	runner := &ClaudeRunner{}
	args := runner.buildRunArgs(
		state.WorkerRole,
		"task",
		"session",
		false,
		"model",
		true,
		"high",
		"prompt",
		runInputs{systemFile: "WORKER.md", schema: "{}"},
	)

	if !containsArgument(args, "Bash") {
		t.Fatalf("read-only worker must still disallow Bash entirely: %#v", args)
	}
	for _, rule := range workerLintDisallowedTools {
		if containsArgument(args, rule) {
			t.Fatalf("read-only worker should use the canonical full Bash deny, not redundant lint rules: %#v", args)
		}
	}
}
