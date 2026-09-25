package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func newDecisionRunner(t *testing.T, commandScript string) *ClaudeRunner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	claudeConfigDir := filepath.Join(t.TempDir(), "claude-home")
	if err := os.MkdirAll(claudeConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"token","ANTHROPIC_BASE_URL":"https://zai.example","UNRELATED_SECRET":"leak"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PARENT_ONLY_ENV", "must-not-leak")
	return NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		RepoShort:       "abcdef123456",
		ClaudeBin:       commandPath,
		ClaudeConfigDir: claudeConfigDir,
		EnvAllowlist:    []string{"GLM_ARGS_FILE"},
	}, newTestStateStore(t))
}

func newDecisionFixture(t *testing.T, resultLine string) (*ClaudeRunner, string, string) {
	t.Helper()
	dir := t.TempDir()
	argumentsPath := filepath.Join(dir, "args")
	environmentPath := filepath.Join(dir, "env")
	stdinPath := argumentsPath + ".stdin"
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"" + argumentsPath + "\"\nenv >\"" + environmentPath + "\"\ncat >\"" + stdinPath + "\"\nprintf '%s\\n' '" + resultLine + "'\n"
	return newDecisionRunner(t, script), argumentsPath, environmentPath
}

func TestDecideRunsIsolatedStructuredCall(t *testing.T) {
	r, argumentsPath, environmentPath := newDecisionFixture(t, `{"type":"result","subtype":"success","is_error":false,"structured_output":{"decisions":[]},"result":"ok","duration_ms":42,"duration_api_ms":40,"total_cost_usd":0.5,"usage":{"input_tokens":10,"cache_read_input_tokens":5,"output_tokens":3}}`)

	result, err := r.Decide("opus", "low", "schema-json", "prompt-json")
	if err != nil {
		t.Fatalf("decide error: %v", err)
	}
	if result.DurationMS != 42 || result.DurationAPIMS != 40 || result.TotalCostUSD != 0.5 {
		t.Fatalf("decision metrics = %#v", result)
	}
	if result.Usage.InputTokens != 10 || result.Usage.CacheReadInputTokens != 5 || result.Usage.OutputTokens != 3 {
		t.Fatalf("decision usage = %#v", result.Usage)
	}
	if string(result.StructuredOutput) != `{"decisions":[]}` {
		t.Fatalf("structured output = %s", result.StructuredOutput)
	}

	args := readLines(t, argumentsPath)
	if !containsArgument(args, "--no-session-persistence") || !containsArgument(args, "--safe-mode") {
		t.Fatalf("decision隔離flag不足: %#v", args)
	}
	if argumentAfter(args, "--setting-sources") != "" {
		t.Fatalf("decisionはsetting-sourcesを空にする必要があります: %#v", args)
	}
	if got := argumentAfter(args, "--tools"); got != "" {
		t.Fatalf("decisionは全toolを無効化すべき: got=%q: %#v", got, args)
	}
	if got := argumentAfter(args, "--json-schema"); got != "schema-json" {
		t.Fatalf("decision json-schema = %q: %#v", got, args)
	}
	if got := argumentAfter(args, "--model"); got != "opus" {
		t.Fatalf("decision model = %q: %#v", got, args)
	}
	if got := argumentAfter(args, "--effort"); got != "low" {
		t.Fatalf("decision effort = %q: %#v", got, args)
	}
	if got := argumentAfter(args, "--mcp-config"); got != `{"mcpServers":{}}` {
		t.Fatalf("decision MCP = %q: %#v", got, args)
	}
	if containsArgument(args, "prompt-json") {
		t.Fatalf("decision promptをargvへ載せるべきではありません: %#v", args)
	}
	stdin, err := os.ReadFile(argumentsPath + ".stdin")
	if err != nil {
		t.Fatalf("decision stdin capture: %v", err)
	}
	if string(stdin) != "prompt-json" {
		t.Fatalf("decision prompt stdin = %q", stdin)
	}

	environment := readLines(t, environmentPath)
	if !containsArgument(environment, "ANTHROPIC_AUTH_TOKEN=token") || !containsArgument(environment, "ANTHROPIC_BASE_URL=https://zai.example") {
		t.Fatalf("decision必須envが選別されていません: %#v", environment)
	}
	if containsArgument(environment, "UNRELATED_SECRET=leak") {
		t.Fatalf("decisionが設定envの非必須値を読み込みました: %#v", environment)
	}
	if containsArgument(environment, "PARENT_ONLY_ENV=must-not-leak") {
		t.Fatalf("decisionが親envを継承しました: %#v", environment)
	}
	if !containsEnvPrefix(environment, "CLAUDE_CONFIG_DIR=") {
		t.Fatalf("decisionへCLAUDE_CONFIG_DIRが渡っていません: %#v", environment)
	}
	if len(result.SettingEnvKeys) != 2 || result.SettingEnvKeys[0] != "ANTHROPIC_AUTH_TOKEN" || result.SettingEnvKeys[1] != "ANTHROPIC_BASE_URL" {
		t.Fatalf("setting env keys = %#v", result.SettingEnvKeys)
	}
}

func TestDecideRejectsMissingStructuredOutput(t *testing.T) {
	r, _, _ := newDecisionFixture(t, `{"type":"result","subtype":"success","is_error":false,"result":"plain","usage":{"input_tokens":1,"output_tokens":1}}`)

	_, err := r.Decide("opus", "low", "schema-json", "prompt-json")
	if !IsStructuredOutputError(err) {
		t.Fatalf("StructuredOutputErrorを期待: %v", err)
	}
}

func TestDecideRejectsProviderErrorResult(t *testing.T) {
	r, _, _ := newDecisionFixture(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"provider down"}`)

	_, err := r.Decide("opus", "low", "schema-json", "prompt-json")
	var callFailure *DecisionCallError
	if !errors.As(err, &callFailure) || callFailure.Reason != "provider-is-error" {
		t.Fatalf("provider-is-errorを期待: %v", err)
	}
}

func TestDecideFailureStaysTypedAndOmitsRawProviderOutput(t *testing.T) {
	secret := "sk-shadow-secret-token"
	cases := []struct {
		name   string
		script string
		reason string
	}{
		{
			name:   "exit",
			script: "#!/bin/sh\nprintf '%s\\n' '" + secret + " via stderr' >&2\nprintf '%s\\n' '" + secret + " via stdout'\nexit 3\n",
			reason: "exit-status",
		},
		{
			name:   "parse",
			script: "#!/bin/sh\nprintf '%s\\n' '" + secret + " via stderr' >&2\nprintf 'not-json'\n",
			reason: "result-parse-failure",
		},
	}
	for _, testCase := range cases {
		r := newDecisionRunner(t, testCase.script)
		_, err := r.Decide("opus", "low", "schema-json", "prompt-json")
		var callFailure *DecisionCallError
		if !errors.As(err, &callFailure) {
			t.Fatalf("%s: DecisionCallErrorを期待: %v", testCase.name, err)
		}
		if callFailure.Reason != testCase.reason {
			t.Fatalf("%s: reason = %q want %q", testCase.name, callFailure.Reason, testCase.reason)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%s: decision失敗がprovider生出力を含みました: %v", testCase.name, err)
		}
	}
}

func containsEnvPrefix(environment []string, prefix string) bool {
	for _, line := range environment {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func TestDecideRequiresModelSchemaPrompt(t *testing.T) {
	r, _, _ := newDecisionFixture(t, `{"type":"result","subtype":"success","is_error":false,"structured_output":{"decisions":[]}}`)
	if _, err := r.Decide("", "low", "schema", "prompt"); err == nil || !strings.Contains(err.Error(), "decision model") {
		t.Fatalf("model必須: %v", err)
	}
	if _, err := r.Decide("opus", "low", "", "prompt"); err == nil || !strings.Contains(err.Error(), "decision schema") {
		t.Fatalf("schema必須: %v", err)
	}
	if _, err := r.Decide("opus", "low", "schema", ""); err == nil || !strings.Contains(err.Error(), "decision prompt") {
		t.Fatalf("prompt必須: %v", err)
	}
}
