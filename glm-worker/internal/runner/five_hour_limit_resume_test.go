package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newFiveHourLimitResumeFixture(t *testing.T, role state.SessionRole) (*ClaudeRunner, *state.StateStore, string) {
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
	commandScript := "#!/bin/sh\nn=$(cat \"$GLM_ARGS_DIR/count\" 2>/dev/null || echo 0)\nn=$((n+1))\nprintf '%s\\n' \"$n\" >\"$GLM_ARGS_DIR/count\"\nprintf '%s\\n' \"$@\" >\"$GLM_ARGS_DIR/run-$n\"\nif [ \"$n\" -eq 1 ]; then\n  printf '%s\\n' 'API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34]'\n  exit 1\nfi\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_ARGS_DIR", argsDir)

	st := newTestStateStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		RepoShort:       "testrepo1234",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: filepath.Join(t.TempDir(), "claude-home"),
		EnvAllowlist:    []string{"GLM_ARGS_DIR"},
	}, st)
	if role == state.ReviewerRole {

		r.config.ReviewerModel = "reviewer-model"
	}
	return r, st, argsDir
}

func testFirstRunFiveHourLimitResumesSameSession(t *testing.T, role state.SessionRole, phase, model string, resumed bool, firstPrompt, resumePrompt string) {
	t.Helper()
	r, st, argsDir := newFiveHourLimitResumeFixture(t, role)

	if _, err := r.Run(role, phase, model, resumed, "high", firstPrompt,
		filepath.Join(t.TempDir(), "first.log")); err == nil {
		t.Fatal("5h上限はerrorを返す必要があります")
	}
	if err := st.MarkReady(role); err != nil {
		t.Fatal(err)
	}
	if policy := st.IsolationPolicy(); policy != isolationPolicyVersion {
		t.Fatalf("5h上限後policy = %q, want %q", policy, isolationPolicyVersion)
	}

	firstArgs := readLines(t, filepath.Join(argsDir, "run-1"))
	firstSessionID := argumentAfter(firstArgs, "--session-id")
	if firstSessionID == "" || containsArgument(firstArgs, "--resume") {
		t.Fatalf("初回は新session採番が必要: %#v", firstArgs)
	}

	if _, err := r.Run(role, phase, model, resumed, "high", resumePrompt,
		filepath.Join(t.TempDir(), "resume.log")); err != nil {
		t.Fatal(err)
	}
	resumeArgs := readLines(t, filepath.Join(argsDir, "run-2"))
	if !containsArgument(resumeArgs, "--resume") || containsArgument(resumeArgs, "--session-id") {
		t.Fatalf("resume呼出しは--resumeで同一sessionへ戻る必要があります: %#v", resumeArgs)
	}
	if got := argumentAfter(resumeArgs, "--resume"); got != firstSessionID {
		t.Fatalf("resume session ID = %q, want %q (同一sessionへ継続)", got, firstSessionID)
	}
}

func TestFirstWorkerRunFiveHourLimitResumesSameSession(t *testing.T) {
	testFirstRunFiveHourLimitResumesSameSession(t, state.WorkerRole, "worker-new", "worker-model", false, "first prompt", "resume prompt")
}

func TestFirstReviewerRunFiveHourLimitResumesSameSession(t *testing.T) {
	testFirstRunFiveHourLimitResumesSameSession(t, state.ReviewerRole, "reviewer-1", "reviewer-model", true, "first review prompt", "resume review prompt")
}
