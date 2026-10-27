package runner

import (
	"slices"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildRunArgsDisallowsMutableWorkerDirectLintCommands(t *testing.T) {
	t.Parallel()
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

	if !containsArgument(args, "--disallowedTools") {
		t.Fatalf("worker args do not contain --disallowedTools: %#v", args)
	}
	for _, rule := range workerLintDisallowedBashRules {
		if !containsArgument(args, rule) {
			t.Fatalf("worker lint deny rule %q is missing: %#v", rule, args)
		}
	}
	for _, rule := range workerGoTestDisallowedBashRules {
		if containsArgument(args, rule) {
			t.Fatalf("Docker policy無効なworkerへGo test deny %qが漏れています: %#v", rule, args)
		}
	}
	if containsArgument(args, "Bash") {
		t.Fatalf("mutable worker Bash tool was removed instead of narrowly denying commands: %#v", args)
	}
}

func TestBuildRunArgsDeniesGoTestOnlyUnderDockerPolicy(t *testing.T) {
	t.Parallel()
	runner := &ClaudeRunner{goTestDockerPolicy: true}
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

	want := append(slices.Clone(workerLintDisallowedBashRules), workerGoTestDisallowedBashRules...)
	for _, rule := range want {
		if !containsArgument(args, rule) {
			t.Fatalf("worker deny rule %q is missing: %#v", rule, args)
		}
	}
	for _, rule := range []string{"Bash(*/go test*)", "Bash(*/go -C* test*)", "Bash(* go1.* test*)", "Bash(*/go1.* -C* test*)"} {
		if !containsArgument(args, rule) {
			t.Fatalf("絶対path・versioned実行形式のdeny %qがありません: %#v", rule, args)
		}
	}
	if containsArgument(args, "Bash") {
		t.Fatalf("mutable worker Bash tool was removed instead of narrowly denying commands: %#v", args)
	}
	if !slices.Equal(workerDisallowedBashRules(true), want) {
		t.Fatalf("policy有効時のdeny一覧 = %#v", workerDisallowedBashRules(true))
	}
}

func TestBuildRunArgsKeepsReadOnlyBashDenyCanonical(t *testing.T) {
	t.Parallel()
	runner := &ClaudeRunner{goTestDockerPolicy: true}
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
	for _, rule := range workerDisallowedBashRules(true) {
		if containsArgument(args, rule) {
			t.Fatalf("read-only worker should use the canonical full Bash deny, not redundant rules: %#v", args)
		}
	}
}
