package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type isolationMigrationFixture struct {
	runner  *ClaudeRunner
	state   *state.StateStore
	argsDir string
}

func TestClaudeRunnerReMintSessionOnStaleIsolationPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	argumentsPath := filepath.Join(t.TempDir(), "args")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_FILE\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_FILE", argumentsPath)

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	staleSession := "stale-session-id"
	if err := st.Write("worker.id", staleSession); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(state.WorkerRole); err != nil {
		t.Fatal(err)
	}
	if err := st.SetIsolationPolicy("claude-isolation-stale"); err != nil {
		t.Fatal(err)
	}

	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
		EnvAllowlist:    []string{"GLM_ARGS_FILE"},
	}, st)

	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "out")); err != nil {
		t.Fatal(err)
	}

	arguments := readLines(t, argumentsPath)
	if containsArgument(arguments, "--resume") {
		t.Fatalf("旧policy sessionをresumeしました: %#v", arguments)
	}
	if !containsArgument(arguments, "--session-id") {
		t.Fatalf("新session採番がありません: %#v", arguments)
	}
	if containsArgument(arguments, staleSession) {
		t.Fatalf("旧session idが再利用されています: %#v", arguments)
	}
	if policy := st.IsolationPolicy(); policy != isolationPolicyVersion {
		t.Fatalf("policy = %q, want %q", policy, isolationPolicyVersion)
	}
}

func newIsolationMigrationFixture(t *testing.T) isolationMigrationFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	argsDir := filepath.Join(t.TempDir(), "args")
	if err := os.MkdirAll(argsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nn=$(cat \"$GLM_ARGS_DIR/count\" 2>/dev/null || echo 0)\nn=$((n+1))\nprintf '%s\\n' \"$n\" >\"$GLM_ARGS_DIR/count\"\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_DIR/run-$n\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_DIR", argsDir)

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
		EnvAllowlist:    []string{"GLM_ARGS_DIR"},
	}, st)
	return isolationMigrationFixture{runner: r, state: st, argsDir: argsDir}
}

func (f isolationMigrationFixture) invocationArgs(t *testing.T, invocation int) []string {
	t.Helper()
	return readLines(t, filepath.Join(f.argsDir, fmt.Sprintf("run-%d", invocation)))
}

func seedStaleReadyRole(t *testing.T, st *state.StateStore, role state.SessionRole, id string) {
	t.Helper()
	if err := st.Write(string(role)+".id", id); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReady(role); err != nil {
		t.Fatal(err)
	}
	if err := st.SetIsolationPolicy("claude-isolation-stale"); err != nil {
		t.Fatal(err)
	}
}

func TestIsolationMigrationWorkerFirstClearsReviewerSession(t *testing.T) {
	f := newIsolationMigrationFixture(t)
	seedStaleReadyRole(t, f.state, state.WorkerRole, "stale-worker")
	seedStaleReadyRole(t, f.state, state.ReviewerRole, "stale-reviewer")

	if _, err := f.runner.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "worker prompt",
		filepath.Join(t.TempDir(), "worker.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "reviewer prompt",
		filepath.Join(t.TempDir(), "reviewer.log")); err != nil {
		t.Fatal(err)
	}

	workerArgs := f.invocationArgs(t, 1)
	reviewerArgs := f.invocationArgs(t, 2)
	if containsArgument(workerArgs, "--resume") || containsArgument(workerArgs, "stale-worker") {
		t.Fatalf("workerが旧sessionをresume/再利用: %#v", workerArgs)
	}
	if !containsArgument(workerArgs, "--session-id") {
		t.Fatalf("workerの新session採番がありません: %#v", workerArgs)
	}
	if containsArgument(reviewerArgs, "--resume") || containsArgument(reviewerArgs, "stale-reviewer") {
		t.Fatalf("reviewerが旧sessionをresume/再利用: %#v", reviewerArgs)
	}
	if !containsArgument(reviewerArgs, "--session-id") {
		t.Fatalf("reviewerの新session採番がありません: %#v", reviewerArgs)
	}
	if policy := f.state.IsolationPolicy(); policy != isolationPolicyVersion {
		t.Fatalf("policy = %q, want %q", policy, isolationPolicyVersion)
	}
}

func TestIsolationMigrationReviewerFirstClearsWorkerSession(t *testing.T) {
	f := newIsolationMigrationFixture(t)
	seedStaleReadyRole(t, f.state, state.WorkerRole, "stale-worker")
	seedStaleReadyRole(t, f.state, state.ReviewerRole, "stale-reviewer")

	if _, err := f.runner.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "reviewer prompt",
		filepath.Join(t.TempDir(), "reviewer.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "worker prompt",
		filepath.Join(t.TempDir(), "worker.log")); err != nil {
		t.Fatal(err)
	}

	reviewerArgs := f.invocationArgs(t, 1)
	workerArgs := f.invocationArgs(t, 2)
	if containsArgument(reviewerArgs, "--resume") || containsArgument(reviewerArgs, "stale-reviewer") {
		t.Fatalf("reviewerが旧sessionをresume/再利用: %#v", reviewerArgs)
	}
	if !containsArgument(reviewerArgs, "--session-id") {
		t.Fatalf("reviewerの新session採番がありません: %#v", reviewerArgs)
	}
	if containsArgument(workerArgs, "--resume") || containsArgument(workerArgs, "stale-worker") {
		t.Fatalf("workerが旧sessionをresume/再利用: %#v", workerArgs)
	}
	if !containsArgument(workerArgs, "--session-id") {
		t.Fatalf("workerの新session採番がありません: %#v", workerArgs)
	}
}

func TestIsolationMigrationClearsNonCallingReadyRole(t *testing.T) {
	f := newIsolationMigrationFixture(t)
	seedStaleReadyRole(t, f.state, state.WorkerRole, "stale-worker")

	if err := f.state.Write("reviewer.id", "stale-reviewer"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.runner.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "reviewer prompt",
		filepath.Join(t.TempDir(), "reviewer.log")); err != nil {
		t.Fatal(err)
	}

	if f.state.Exists("worker.ready") {
		t.Fatal("呼出し対象でないworkerの旧readyが残っています")
	}
	if _, err := f.runner.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "worker prompt",
		filepath.Join(t.TempDir(), "worker.log")); err != nil {
		t.Fatal(err)
	}

	workerArgs := f.invocationArgs(t, 2)
	if containsArgument(workerArgs, "--resume") || containsArgument(workerArgs, "stale-worker") {
		t.Fatalf("workerが旧sessionをresume/再利用: %#v", workerArgs)
	}
	if !containsArgument(workerArgs, "--session-id") {
		t.Fatalf("workerの新session採番がありません: %#v", workerArgs)
	}
}

func TestIsolationPolicyPersistedBeforeExecutionOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	argsDir := filepath.Join(t.TempDir(), "args")
	if err := os.MkdirAll(argsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nn=$(cat \"$GLM_ARGS_DIR/count\" 2>/dev/null || echo 0)\nn=$((n+1))\nprintf '%s\\n' \"$n\" >\"$GLM_ARGS_DIR/count\"\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_DIR/run-$n\"\nif [ \"$n\" -eq 1 ]; then\n  printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error\",\"is_error\":true,\"result\":\"boom\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n  exit 1\nfi\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_DIR", argsDir)

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
		EnvAllowlist:    []string{"GLM_ARGS_DIR"},
	}, st)
	seedStaleReadyRole(t, st, state.WorkerRole, "stale-worker")

	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "first prompt",
		filepath.Join(t.TempDir(), "first.log")); err == nil {
		t.Fatal("1回目は失敗する必要があります")
	}
	if policy := st.IsolationPolicy(); policy != isolationPolicyVersion {
		t.Fatalf("実行前永続化により失敗時もpolicy = %qが期待: %q", isolationPolicyVersion, policy)
	}
	if st.Exists("worker.ready") {
		t.Fatal("失敗時にworker.readyを書いてはいけません")
	}
	failedSessionID, err := st.Read("worker.id")
	if err != nil {
		t.Fatalf("失敗時のsession idを読めません: %v", err)
	}

	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "retry prompt",
		filepath.Join(t.TempDir(), "retry.log")); err != nil {
		t.Fatal(err)
	}

	retryArgs := readLines(t, filepath.Join(argsDir, "run-2"))
	if containsArgument(retryArgs, "--resume") {
		t.Fatalf("未readyの失敗sessionをresumeしました: %#v", retryArgs)
	}
	if !containsArgument(retryArgs, "--session-id") {
		t.Fatalf("session id指定がありません: %#v", retryArgs)
	}
	if !containsArgument(retryArgs, failedSessionID) {
		t.Fatalf("runner層は失敗session idを保持する必要があります(runner破棄はworkflow層): %#v", retryArgs)
	}
	if policy := st.IsolationPolicy(); policy != isolationPolicyVersion {
		t.Fatalf("成功後policy = %q, want %q", policy, isolationPolicyVersion)
	}
}

func TestIsolationPolicyWriteFailureAbortsBeforeClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	invokedPath := filepath.Join(t.TempDir(), "claude-invoked")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '1' >\"" + invokedPath + "\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(st.Path("isolation.policy"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
	}, st)

	_, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "out.log"))
	if err == nil {
		t.Fatal("policy永続化失敗時はerrorを返す必要があります")
	}
	if _, statErr := os.Stat(invokedPath); statErr == nil {
		t.Fatal("policy永続化失敗時にClaudeを実行しました")
	}
}

func assertFullIsolationArgs(t *testing.T, args []string, claudeConfigDir string, expectReviewerAgentBlock bool) {
	t.Helper()
	if !containsArgument(args, "--safe-mode") {
		t.Fatalf("--safe-modeがありません: %#v", args)
	}
	if argumentAfter(args, "--setting-sources") != "" {
		t.Fatalf("setting-sourcesを空にする必要があります: %#v", args)
	}
	if !containsArgument(args, "--strict-mcp-config") {
		t.Fatalf("--strict-mcp-configがありません: %#v", args)
	}
	if got := argumentAfter(args, "--mcp-config"); got != `{"mcpServers":{}}` {
		t.Fatalf("--mcp-config = %q: %#v", got, args)
	}
	if !containsArgument(args, "--disable-slash-commands") {
		t.Fatalf("--disable-slash-commandsがありません: %#v", args)
	}
	settingsValue := argumentAfter(args, "--settings")
	if settingsValue == "" {
		t.Fatalf("隔離--settingsがありません: %#v", args)
	}
	var payload struct {
		ClaudeMdExcludes     []string `json:"claudeMdExcludes"`
		AutoMemoryEnabled    bool     `json:"autoMemoryEnabled"`
		DisableAllHooks      bool     `json:"disableAllHooks"`
		DisableBundledSkills bool     `json:"disableBundledSkills"`
		DisableWorkflows     bool     `json:"disableWorkflows"`
	}
	if err := json.Unmarshal([]byte(settingsValue), &payload); err != nil {
		t.Fatalf("隔離--settingsがJSONではありません: %v: %q", err, settingsValue)
	}
	wantRules := filepath.Join(claudeConfigDir, "rules", "**")
	wantUserGlobal := filepath.Join(claudeConfigDir, "CLAUDE.md")
	if !containsString(payload.ClaudeMdExcludes, "**/CLAUDE.md") ||
		!containsString(payload.ClaudeMdExcludes, "**/CLAUDE.local.md") ||
		!containsString(payload.ClaudeMdExcludes, wantUserGlobal) ||
		!containsString(payload.ClaudeMdExcludes, wantRules) {
		t.Fatalf("claudeMdExcludesが不完全: %#v", payload.ClaudeMdExcludes)
	}
	if payload.AutoMemoryEnabled || !payload.DisableAllHooks || !payload.DisableBundledSkills || !payload.DisableWorkflows {
		t.Fatalf("customization無効化が不完全: %#v", payload)
	}

	hasAgentDisallowed := containsArgument(args, "Agent")
	if expectReviewerAgentBlock != hasAgentDisallowed {
		t.Fatalf("reviewer Agent禁止/worker Agent許可が期待と違います(expect=%v): %#v", expectReviewerAgentBlock, args)
	}
	if expectReviewerAgentBlock {
		for _, blocked := range []string{"Edit", "Write", "NotebookEdit", "Agent"} {
			if !containsArgument(args, blocked) {
				t.Fatalf("reviewerのdisallowedTools%qがありません: %#v", blocked, args)
			}
		}
	}
}

func TestIsolationArgsIdenticalAcrossRoleAndResume(t *testing.T) {
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
	argsDir := filepath.Join(t.TempDir(), "args")
	if err := os.MkdirAll(argsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")

	commandScript := "#!/bin/sh\nn=$(cat \"$GLM_ARGS_DIR/count\" 2>/dev/null || echo 0)\nn=$((n+1))\nprintf '%s\\n' \"$n\" >\"$GLM_ARGS_DIR/count\"\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_DIR/run-$n\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_DIR", argsDir)

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	claudeConfigDir := filepath.Join(t.TempDir(), "claude-home")
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        repository,
		RepoShort:       "abcdef123456",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: claudeConfigDir,
		EnvAllowlist:    []string{"GLM_ARGS_DIR"},
	}, st)

	paths := []struct {
		name           string
		role           state.SessionRole
		model          string
		readOnly       bool
		expectReviewer bool
	}{
		{"worker-new", state.WorkerRole, "worker-model", false, false},
		{"worker-resume", state.WorkerRole, "worker-model", false, false},
		{"reviewer-new", state.ReviewerRole, "reviewer-model", true, true},
		{"reviewer-resume", state.ReviewerRole, "reviewer-model", true, true},
	}
	for _, step := range paths {
		if _, err := r.Run(step.role, step.name, step.model, step.readOnly, "high", step.name+" prompt", filepath.Join(t.TempDir(), step.name+".log")); err != nil {
			t.Fatalf("%s Run error: %v", step.name, err)
		}
	}

	for index, step := range paths {
		args := readLines(t, filepath.Join(argsDir, fmt.Sprintf("run-%d", index+1)))
		if !containsArgument(args, step.name+" prompt") {
			t.Fatalf("%s: prompt引数が記録されていません: %#v", step.name, args)
		}
		assertFullIsolationArgs(t, args, claudeConfigDir, step.expectReviewer)
		if strings.HasSuffix(step.name, "-new") {
			if !containsArgument(args, "--session-id") || containsArgument(args, "--resume") {
				t.Fatalf("%s: 新規session引数が不正: %#v", step.name, args)
			}
		} else {
			if !containsArgument(args, "--resume") || containsArgument(args, "--session-id") {
				t.Fatalf("%s: resume引数が不正: %#v", step.name, args)
			}
		}
	}
}
