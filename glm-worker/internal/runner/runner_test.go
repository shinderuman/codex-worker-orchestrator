package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runnerSessionFixture struct {
	runner          *ClaudeRunner
	argumentsPath   string
	claudeConfigDir string
}

func newTestStateStore(t *testing.T) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(config.AppConfig{
		StateBase: t.TempDir(),
		RepoHash:  "runnerhash",
		RepoRoot:  "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSessionNameIncludesTaskID(t *testing.T) {
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := &ClaudeRunner{
		config: config.AppConfig{RepoShort: "abcdef123456"},
		state:  st,
	}

	got := r.sessionName(state.WorkerRole, "12345678-aaaa-bbbb-cccc-dddddddddddd")
	want := "glm-worker-abcdef123456-12345678"
	if got != want {
		t.Fatalf("session name = %q, want %q", got, want)
	}
}

func newRunnerSessionFixture(t *testing.T) runnerSessionFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}

	repository := t.TempDir()
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	argumentsPath := filepath.Join(t.TempDir(), "args")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_FILE\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"runner output\\n\",\"duration_ms\":1200,\"duration_api_ms\":900,\"num_turns\":2,\"usage\":{\"input_tokens\":11,\"cache_creation_input_tokens\":12,\"cache_read_input_tokens\":13,\"output_tokens\":14},\"modelUsage\":{\"glm-5.3\":{\"inputTokens\":11,\"cacheCreationInputTokens\":12,\"cacheReadInputTokens\":13,\"outputTokens\":14}}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_FILE", argumentsPath)

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	claudeConfigDir := filepath.Join(t.TempDir(), "claude-home")

	return runnerSessionFixture{
		runner: NewClaudeRunner(config.AppConfig{
			RepoRoot:        repository,
			RepoShort:       "abcdef123456",
			PromptDir:       promptDir,
			ClaudeBin:       commandPath,
			ClaudeConfigDir: claudeConfigDir,
			EnvAllowlist:    []string{"GLM_ARGS_FILE"},
			WorkerModel:     "worker-model",
			ReviewerModel:   "reviewer-model",
		}, st),
		argumentsPath:   argumentsPath,
		claudeConfigDir: claudeConfigDir,
	}
}

func (f runnerSessionFixture) runFirst(t *testing.T) (RunResult, string) {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "first.log")
	result, err := f.runner.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "first prompt", outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return result, outputPath
}

func TestClaudeRunnerRunStartsSessionWithIsolatedArguments(t *testing.T) {
	fixture := newRunnerSessionFixture(t)
	_, _ = fixture.runFirst(t)

	arguments := readLines(t, fixture.argumentsPath)
	if !containsArgument(arguments, "--session-id") || containsArgument(arguments, "--resume") {
		t.Fatalf("初回引数 = %#v", arguments)
	}
	if !containsArgument(arguments, "worker-model") || !containsArgument(arguments, "first prompt") {
		t.Fatalf("worker引数 = %#v", arguments)
	}
	if !containsArgument(arguments, "stream-json") || !containsArgument(arguments, "--verbose") {
		t.Fatalf("stream-json出力指定がありません: %#v", arguments)
	}
	settingsValue := argumentAfter(arguments, "--settings")
	if settingsValue == "" {
		t.Fatalf("隔離--settingsがありません: %#v", arguments)
	}

	var settingsPayload struct {
		ClaudeMdExcludes     []string `json:"claudeMdExcludes"`
		AutoMemoryEnabled    bool     `json:"autoMemoryEnabled"`
		DisableAllHooks      bool     `json:"disableAllHooks"`
		DisableBundledSkills bool     `json:"disableBundledSkills"`
		DisableWorkflows     bool     `json:"disableWorkflows"`
	}
	if err := json.Unmarshal([]byte(settingsValue), &settingsPayload); err != nil {
		t.Fatalf("--settingsの値がJSONではありません: %v: %q", err, settingsValue)
	}
	wantRules := filepath.Join(fixture.claudeConfigDir, "rules", "**")
	wantUserGlobal := filepath.Join(fixture.claudeConfigDir, "CLAUDE.md")
	if !containsString(settingsPayload.ClaudeMdExcludes, "**/CLAUDE.md") ||
		!containsString(settingsPayload.ClaudeMdExcludes, "**/CLAUDE.local.md") ||
		!containsString(settingsPayload.ClaudeMdExcludes, wantUserGlobal) ||
		!containsString(settingsPayload.ClaudeMdExcludes, wantRules) {
		t.Fatalf("claudeMdExcludes = %#v", settingsPayload.ClaudeMdExcludes)
	}
	if settingsPayload.AutoMemoryEnabled || !settingsPayload.DisableAllHooks || !settingsPayload.DisableBundledSkills || !settingsPayload.DisableWorkflows {
		t.Fatalf("隔離settings = %#v", settingsPayload)
	}
	if !containsArgument(arguments, "--safe-mode") {
		t.Fatalf("--safe-modeがありません: %#v", arguments)
	}
	if argumentAfter(arguments, "--setting-sources") != "" {
		t.Fatalf("setting-sourcesを空にする必要があります: %#v", arguments)
	}
	if !containsArgument(arguments, "--strict-mcp-config") {
		t.Fatalf("--strict-mcp-configがありません: %#v", arguments)
	}
	if got := argumentAfter(arguments, "--mcp-config"); got != `{"mcpServers":{}}` {
		t.Fatalf("--mcp-config = %q", got)
	}
	if !containsArgument(arguments, "--disable-slash-commands") {
		t.Fatalf("--disable-slash-commandsがありません: %#v", arguments)
	}
}

func TestClaudeRunnerRunParsesResultUsage(t *testing.T) {
	fixture := newRunnerSessionFixture(t)
	result, outputPath := fixture.runFirst(t)

	output, err := os.ReadFile(outputPath)
	if err != nil || string(output) != "runner output\n" {
		t.Fatalf("output = %q, err = %v", output, err)
	}
	if result.TopLevelUsage.InputTokens != 11 || result.TopLevelUsage.CacheReadInputTokens != 13 || result.TopLevelUsage.OutputTokens != 14 {
		t.Fatalf("usage = %#v", result.TopLevelUsage)
	}
	if result.ModelUsage["glm-5.3"].OutputTokens != 14 || result.SystemPromptSHA256 == "" || result.SystemPrompt != "system" {
		t.Fatalf("run result = %#v", result)
	}
}

func TestClaudeRunnerRunResumesSessionReadOnly(t *testing.T) {
	fixture := newRunnerSessionFixture(t)
	_, _ = fixture.runFirst(t)

	outputPath := filepath.Join(t.TempDir(), "second.log")
	result, err := fixture.runner.Run(state.WorkerRole, "worker-decision", "override-model", true, "max", "second prompt", outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Resumed {
		t.Fatal("2回目がresumeとして記録されていません")
	}

	arguments := readLines(t, fixture.argumentsPath)
	if !containsArgument(arguments, "--resume") || containsArgument(arguments, "--session-id") {
		t.Fatalf("resume引数 = %#v", arguments)
	}
	for _, argument := range []string{"--disallowedTools", "Edit", "Write", "NotebookEdit", "Agent", "Bash", "second prompt"} {
		if !containsArgument(arguments, argument) {
			t.Fatalf("read-only引数%qがありません: %#v", argument, arguments)
		}
	}
	if got := argumentAfter(arguments, "--tools"); got != "Read,Grep,Glob,WebFetch,WebSearch" {
		t.Fatalf("read-only --tools = %q", got)
	}
	if !containsArgument(arguments, "override-model") {
		t.Fatalf("model overrideがありません: %#v", arguments)
	}
}

func TestClaudeRunnerRejectsMissingPrompt(t *testing.T) {
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		PromptDir: t.TempDir(),
		ClaudeBin: "unused",
	}, st)

	_, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "required promptがありません") {
		t.Fatalf("missing prompt error = %v", err)
	}
}

func TestClaudeRunnerRejectsMissingTaskID(t *testing.T) {
	st := newTestStateStore(t)
	r := NewClaudeRunner(config.AppConfig{}, st)

	_, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	if err == nil || !strings.Contains(err.Error(), "task.idがありません") {
		t.Fatalf("missing task ID error = %v", err)
	}
}

func TestClaudeRunnerPreservesErrorResultAndUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	repository := t.TempDir()
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error\",\"is_error\":true,\"result\":\"API Error: Request rejected (429) [1308][Usage limit reached for 5 hour.]\",\"usage\":{\"input_tokens\":5,\"output_tokens\":6}}'\nprintf '%s\\n' 'stderr diagnostic' >&2\nexit 1\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:  repository,
		PromptDir: promptDir,
		ClaudeBin: commandPath,
	}, st)
	outputPath := filepath.Join(t.TempDir(), "error.log")
	result, err := r.Run(state.WorkerRole, "worker-new", "opus", false, "high", "prompt", outputPath)
	if err == nil {
		t.Fatal("exit statusを返す必要があります")
	}
	if result.TopLevelUsage.InputTokens != 5 || result.TopLevelUsage.OutputTokens != 6 {
		t.Fatalf("error usage = %#v", result.TopLevelUsage)
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "Usage limit reached") || !strings.Contains(string(data), "stderr diagnostic") {
		t.Fatalf("error output = %q", data)
	}
}

func TestClaudeRunnerRejectsInvalidJSONWithoutMarkingSessionReady(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	repository := t.TempDir()
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nprintf '%s\\n' 'not json'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:  repository,
		PromptDir: promptDir,
		ClaudeBin: commandPath,
	}, st)
	_, err := r.Run(state.WorkerRole, "worker-new", "opus", false, "high", "prompt", filepath.Join(t.TempDir(), "output.log"))
	if err == nil || !strings.Contains(err.Error(), "result eventがありません") {
		t.Fatalf("invalid JSON error = %v", err)
	}
	if st.Exists("worker.ready") {
		t.Fatal("不正JSONでsessionをreadyにしてはいけません")
	}
}

func TestParseClaudeJSONResultKeepsTopLevelAndTreeUsageSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	data := `{"type":"result","result":"packet","modelUsage":{"glm-4.7":{"inputTokens":3,"cacheCreationInputTokens":4,"cacheReadInputTokens":5,"outputTokens":6},"glm-5.3":{"inputTokens":7,"outputTokens":8}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := parseClaudeJSONResult(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage != (TokenUsage{}) {
		t.Fatalf("top-level usage = %#v", result.Usage)
	}
	if result.ModelUsage["glm-4.7"].InputTokens != 3 || result.ModelUsage["glm-5.3"].OutputTokens != 8 {
		t.Fatalf("model usage = %#v", result.ModelUsage)
	}
}

func TestPromptFileNameByRole(t *testing.T) {
	if got := promptFileName(state.WorkerRole); got != "WORKER.md" {
		t.Fatalf("worker prompt = %q", got)
	}
	if got := promptFileName(state.ReviewerRole); got != "REVIEWER.md" {
		t.Fatalf("reviewer prompt = %q", got)
	}
}

func TestZaiRateLimitErrorMessageMentionsPhase(t *testing.T) {
	err := ZaiRateLimitError{Phase: "reviewer-1"}.Error()
	if !strings.Contains(err, "reviewer-1") {
		t.Fatalf("rate limit errorのmessageにphaseがありません: %s", err)
	}
}

func TestZaiRateLimitErrorAutoResumeSchedule(t *testing.T) {
	limit := ZaiRateLimitError{
		Limit: ZaiFiveHourLimit{ResetAtRFC3339: "2026-08-09T22:35:58+08:00"},
	}
	available, at := limit.AutoResumeSchedule()
	if !available {
		t.Fatalf("reset時刻があるのにauto resume不可: %v", limit)
	}
	if at != "2026-08-09T22:37:58+08:00" {
		t.Fatalf("auto resume予定時刻がgrace反映後と違います: %s", at)
	}

	withoutReset := ZaiRateLimitError{}
	if available, _ := withoutReset.AutoResumeSchedule(); available {
		t.Fatalf("reset時刻がないのにauto resume可: %v", withoutReset)
	}
}

func TestZaiRateLimitErrorAutoResumeKey(t *testing.T) {
	key := ZaiRateLimitError{RepoShort: "abcdef123456", TaskID: "12345678-aaaa-bbbb-cccc-dddddddddddd"}.AutoResumeKey()
	if key != "glm-worker-resume-abcdef123456-12345678" {
		t.Fatalf("auto resume keyが違います: %s", key)
	}
	fallback := ZaiRateLimitError{}.AutoResumeKey()
	if fallback != "glm-worker-resume-unknown-repo-unknown-task" {
		t.Fatalf("fallback keyが違います: %s", fallback)
	}
}
