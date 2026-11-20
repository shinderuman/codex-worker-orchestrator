package app

import (
	"strconv"
	"strings"
	"testing"
)

const stagedTestToken = "stage-token"
const stagedTestDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseCommandModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		mode    CommandMode
		payload string
	}{
		{name: "new task", args: []string{"--task", "調査して 実装する"}, mode: ModeNewTask, payload: "調査して 実装する"},
		{name: "new task with option-shaped text", args: []string{"--task", "調査して --unknown"}, mode: ModeNewTask, payload: "調査して --unknown"},
		{name: "new task preserves bytes", args: []string{"--task", "  調査して\n実装する  "}, mode: ModeNewTask, payload: "  調査して\n実装する  "},
		{name: "resume", args: []string{"--resume"}, mode: ModeResume},
		{name: "stop", args: []string{"--stop"}, mode: ModeStop},
		{name: "status", args: []string{"--status"}, mode: ModeStatus},
		{name: "stats", args: []string{"--stats"}, mode: ModeStats},
		{name: "eval-ab", args: []string{"--eval-ab", "/tmp/ab-run"}, mode: ModeEvalAB, payload: "/tmp/ab-run"},
		{name: "eval-ab-worktree-identity", args: []string{"--eval-ab-worktree-identity", "/tmp/worktree"}, mode: ModeEvalABWorktreeIdentity, payload: "/tmp/worktree"},
		{name: "call-outliers", args: []string{"--call-outliers"}, mode: ModeCallOutliers},
		{name: "model-routing", args: []string{"--model-routing"}, mode: ModeModelRouting},
		{name: "test-impact", args: []string{"--test-impact"}, mode: ModeTestImpact},
		{name: "codex-limit", args: []string{"--codex-limit"}, mode: ModeCodexLimit},
		{name: "repo-search", args: []string{"--repo-search", "worker dispatch", "--scope", "internal/workflow", "--budget", "2048"}, mode: ModeRepoSearch, payload: "worker dispatch"},
		{name: "repo-search-eval", args: []string{"--repo-search-eval"}, mode: ModeRepoSearchEval},
		{name: "review-gap", args: []string{"--review-gap"}, mode: ModeReviewGap},
		{name: "review-gap task", args: []string{"--review-gap", "task-123"}, mode: ModeReviewGap, payload: "task-123"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := ParseCommand(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Mode != test.mode || command.Payload != test.payload {
				t.Fatalf("command = %#v", command)
			}
		})
	}
}

func TestParentUsageCommandIsRetired(t *testing.T) {
	t.Parallel()
	if _, ok := commandParsers["--parent-usage"]; ok {
		t.Fatal("retired --parent-usage command remains registered")
	}
}

func TestIsolateCommandIsRetired(t *testing.T) {
	t.Parallel()
	if _, ok := commandParsers["--isolate"]; ok {
		t.Fatal("retired --isolate command remains registered")
	}
	if _, err := ParseCommand([]string{"--isolate"}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("retired --isolate command is not rejected as unknown: %v", err)
	}
}

func TestParkCommandsAreRetired(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"--park", "--unpark"} {
		if _, ok := commandParsers[name]; ok {
			t.Fatalf("retired %s command remains registered", name)
		}
		if _, err := ParseCommand([]string{name}); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("retired %s command is not rejected as unknown: %v", name, err)
		}
	}
}

func TestResetCommandIsRetired(t *testing.T) {
	t.Parallel()
	if _, ok := commandParsers["--reset"]; ok {
		t.Fatal("retired --reset command remains registered")
	}
	for _, args := range [][]string{{"--reset"}, {"--reset", "--disposition", "abandon"}} {
		if _, err := ParseCommand(args); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("retired --reset command is not rejected as unknown: %v", err)
		}
	}
}

func TestGLMProviderAutoResumeCommandsAreRetired(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"--verify-auto-resume",
		"--check-wake-coalesce",
		"--auto-resume-plan",
		"--auto-resume-response-stdin",
		"--auto-resume-fallback",
	} {
		if _, ok := commandParsers[name]; ok {
			t.Fatalf("retired provider auto-resume command remains registered: %s", name)
		}
	}
}

func stagedTestCommandArgs(action string, options ...string) []string {
	return append([]string{"--staged-parent-input", action, stagedTestToken, "--sha256", stagedTestDigest}, options...)
}

func TestParseCommandStagedParentInput(t *testing.T) {
	t.Parallel()
	for action, mode := range map[string]CommandMode{"decision": ModeDecision, "fix": ModeFix} {
		args := stagedTestCommandArgs(action)
		args[4] = strings.ToUpper(args[4])
		command, err := ParseCommand(args)
		if err != nil || command.Mode != mode || command.StdinBytes != 0 || command.Payload != "" ||
			command.ParentStageToken != stagedTestToken || command.SHA256 != stagedTestDigest {
			t.Fatalf("staged command = %#v, %v", command, err)
		}
	}
}

func TestParseCommandRejectsInvalidStagedInputArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--staged-parent-input"},
		{"--staged-parent-input", "decision", stagedTestToken},
		{"--staged-parent-input", "decision", "", "--sha256", stagedTestDigest},
		{"--staged-parent-input", "decision", stagedTestToken, "--sha256", "short"},
		{"--staged-parent-input", "decision", stagedTestToken, "--sha256", stagedTestDigest[:63] + "g"},
		{"--staged-parent-input", "other", stagedTestToken, "--sha256", stagedTestDigest},
		stagedTestCommandArgs("decision", "unexpected"),
		stagedTestCommandArgs("fix", "--sha256", stagedTestDigest),
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid staged args accepted: %#v", args)
		}
	}
}

func TestRetiredDecisionFixStdinCommandsUseGenericRejection(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--decision-stdin", "--fix-stdin"} {
		_, err := ParseCommand([]string{flag, "12", "--sha256", stagedTestDigest})
		if err == nil || err.Error() != "unknown command "+strconv.Quote(flag)+"; run glm-worker --help for command list" {
			t.Fatalf("retired command must use generic unknown-command handling: %v", err)
		}
	}
}

func TestParseCommandRejectsUnknownCommandShapedFirstToken(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--unknown"},
		{"--unknown", "payload"},
		{"unknown"},
		{"unknown", "payload"},
		{"bundle", "task-id"},
		{"start", "payload"},
		{"-x"},
		{""},
		{"調査して", "実装する"},
		{"--decision", "A案で進める"},
		{"--fix", "指摘を修正"},
	} {
		command, err := ParseCommand(args)
		if err == nil {
			t.Fatalf("unknown command-shaped argvを受理しました: %#v", args)
		}
		if command.Payload != "" {
			t.Fatalf("unknown command-shaped argvをnew-task payloadへ解釈しています: %#v", command)
		}
		if !strings.Contains(err.Error(), "unknown command") || !strings.Contains(err.Error(), "glm-worker --help") {
			t.Fatalf("generic unknown-command usage errorではありません: %v", err)
		}
		if strings.Contains(err.Error(), "--decision-stdin") || strings.Contains(err.Error(), "--fix-stdin") {
			t.Fatalf("retired command-specific migration hintが残っています: %v", err)
		}
	}
}

func TestParseCommandRejectsInvalidNewTask(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--task"},
		{"--task", ""},
		{"--task", " \n\t"},
		{"--task", "調査して", "実装する"},
		{"--task", "調査して", "--unknown"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid new-task argvを受理しました: %#v", args)
		}
	}
}

func TestParseCommandRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		nil,
		{"--resume", "extra"},
		{"--stop", "extra"},
		{"--status", "extra"},
		{"--stats", "extra"},
		{"--verify-codex-wake"},
		{"--verify-codex-wake", "01a03a9e-10a0-7f11-801c-f04e5dbd5490"},
		{"--verify-codex-wake", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", "2026-08-26T15:17:55Z", "extra"},
		{"--verify-codex-wake", "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490", "2026-08-26T15:17:55Z"},
		{"--verify-codex-wake", "01A03A9E-10A0-7F11-801C-F04E5DBD5490", "2026-08-26T15:17:55Z"},
		{"--verify-codex-wake", "not-a-thread-id", "2026-08-26T15:17:55Z"},
		{"--eval-ab"},
		{"--eval-ab", "dir", "extra"},
		{"--call-outliers", "extra"},
		{"--model-routing", "extra"},
		{"--test-impact", "extra"},
		{"--codex-limit", "extra"},
		{"--repo-search"},
		{"--repo-search", "query", "extra"},
		{"--repo-search-eval", "extra"},
		{"--review-gap", "task-1", "extra"},
	}

	for _, args := range tests {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid argsを受理しました: %#v", args)
		}
	}
}

func TestParseCommandVerifyCodexWakeArgs(t *testing.T) {
	t.Parallel()
	command, err := ParseCommand([]string{
		"--verify-codex-wake",
		"01a03a9e-10a0-7f11-801c-f04e5dbd5490",
		"2026-08-12T20:01:20+09:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeVerifyCodexWake {
		t.Fatalf("Mode = %d", command.Mode)
	}
	if command.Verify.ThreadID != "01a03a9e-10a0-7f11-801c-f04e5dbd5490" {
		t.Fatalf("ThreadID = %q", command.Verify.ThreadID)
	}
	if command.Verify.RFC3339 != "2026-08-12T20:01:20+09:00" {
		t.Fatalf("RFC3339 = %q", command.Verify.RFC3339)
	}
}
