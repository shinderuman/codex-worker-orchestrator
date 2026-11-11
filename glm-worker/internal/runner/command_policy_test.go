package runner

import (
	"regexp"
	"slices"
	"strings"
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
	for _, rule := range workerDockerDisallowedBashRules {
		if containsArgument(args, rule) {
			t.Fatalf("Docker policy無効なworkerへDocker deny %qが漏れています: %#v", rule, args)
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
		runInputs{systemFile: "WORKER.md", schema: "{}"},
	)

	want := append(append(slices.Clone(workerLintDisallowedBashRules), workerGoTestDisallowedBashRules...), workerDockerDisallowedBashRules...)
	for _, rule := range want {
		if !containsArgument(args, rule) {
			t.Fatalf("worker deny rule %q is missing: %#v", rule, args)
		}
	}
	for _, rule := range []string{"Bash(*/go test*)", "Bash(*/go -C* test*)", "Bash(go --C* test*)", "Bash(* go1.* test*)", "Bash(*/go1.* -C* test*)", "Bash(docker *)", "Bash(* docker *)", "Bash(*/docker *)"} {
		if !containsArgument(args, rule) {
			t.Fatalf("絶対path・versioned・--C・Docker実行形式のdeny %qがありません: %#v", rule, args)
		}
	}
	if containsArgument(args, "Bash") {
		t.Fatalf("mutable worker Bash tool was removed instead of narrowly denying commands: %#v", args)
	}
	if !slices.Equal(workerDisallowedBashRules(true), want) {
		t.Fatalf("policy有効時のdeny一覧 = %#v", workerDisallowedBashRules(true))
	}
}

func TestWorkerDockerDenyRuleCommandCoverage(t *testing.T) {
	t.Parallel()

	wantRules := []string{"Bash(docker *)", "Bash(* docker *)", "Bash(*/docker *)"}
	if !slices.Equal(workerDockerDisallowedBashRules, wantRules) {
		t.Fatalf("Docker deny rules = %#v, want %#v", workerDockerDisallowedBashRules, wantRules)
	}

	// Check the emitted Bash wildcard patterns against concrete command strings.
	// CLI enforcement still requires a real Claude CLI policy integration check.
	cases := []struct {
		name    string
		command string
		denied  bool
	}{
		{name: "bare-run", command: "docker run --rm alpine", denied: true},
		{name: "bare-exec", command: "docker exec container sh", denied: true},
		{name: "bare-cp", command: "docker cp src container:/dst", denied: true},
		{name: "bare-build", command: "docker build .", denied: true},
		{name: "bare-compose", command: "docker compose up", denied: true},
		{name: "bare-version", command: "docker version", denied: true},
		{name: "bare-pull", command: "docker pull alpine", denied: true},
		{name: "bare-image", command: "docker image ls", denied: true},
		{name: "bare-volume", command: "docker volume ls", denied: true},
		{name: "absolute-run", command: "/usr/local/bin/docker run --rm alpine", denied: true},
		{name: "absolute-version", command: "/opt/homebrew/bin/docker version", denied: true},
		{name: "compound-bare", command: "cd x && docker run --rm alpine", denied: true},
		{name: "compound-absolute", command: "cd x && /usr/local/bin/docker image ls", denied: true},
		{name: "goquality-test", command: "./goquality test ./...", denied: false},
		{name: "docker-lookalike", command: "dockerless run alpine", denied: false},
		{name: "path-lookalike", command: "/usr/local/bin/docker-helper run alpine", denied: false},
		{name: "unrelated-compound", command: "cd x && gofmt -w file.go", denied: false},
		{name: "docker-lookup", command: "command -v docker", denied: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			denied := false
			for _, rule := range workerDockerDisallowedBashRules {
				glob := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
				expr := "^" + strings.ReplaceAll(regexp.QuoteMeta(glob), `\*`, ".*") + "$"
				matched, err := regexp.MatchString(expr, tc.command)
				if err != nil {
					t.Fatalf("invalid glob expression %q: %v", rule, err)
				}
				if matched {
					denied = true
					break
				}
			}
			if denied != tc.denied {
				t.Errorf("command %q: denied=%t, want %t", tc.command, denied, tc.denied)
			}
		})
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
