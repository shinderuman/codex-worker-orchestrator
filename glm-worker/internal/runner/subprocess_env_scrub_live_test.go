package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeSubprocessEnvScrubLiveNoAI(t *testing.T) {
	bin := os.Getenv("GLM_WORKER_CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		t.Skipf("claude CLIが利用できません: %v", err)
	}

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "subprocess-env-report")
	settingsPath := filepath.Join(dir, "settings.json")
	hook := `token=absent; api_key=absent; base_url=absent; report_path=absent; ` +
		`[ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] && token=present; ` +
		`[ -n "${ANTHROPIC_API_KEY:-}" ] && api_key=present; ` +
		`[ -n "${ANTHROPIC_BASE_URL:-}" ] && base_url=present; ` +
		`[ -n "${GLM_SUBPROCESS_SCRUB_REPORT:-}" ] && report_path=present; ` +
		`printf 'auth_token=%s\napi_key=%s\nbase_url=%s\nreport_path=%s\n' "$token" "$api_key" "$base_url" "$report_path" > "$GLM_SUBPROCESS_SCRUB_REPORT"`
	settings, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": hook}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		t.Fatal(err)
	}

	settingEnv := map[string]string{
		"ANTHROPIC_AUTH_TOKEN": "glm-worker-scrub-token",
		"ANTHROPIC_API_KEY":    "glm-worker-scrub-api-key",
		"ANTHROPIC_BASE_URL":   "http://127.0.0.1:1",
	}
	additions := claudeInvocationEnvDefaults()
	additions["CLAUDE_CONFIG_DIR"] = filepath.Join(dir, "claude-config")
	additions["GLM_SUBPROCESS_SCRUB_REPORT"] = reportPath

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, resolved,
		"-p", "--setting-sources", "", "--no-session-persistence",
		"--model", "opus", "--output-format", "json",
		"--dangerously-skip-permissions", "--settings", settingsPath,
		"--tools", "", "reply with ok",
	)
	command.Dir = dir
	command.Env = buildChildEnv(nil, settingEnv, additions, nil)
	_ = command.Run()

	report, err := os.ReadFile(reportPath)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("claude subprocess scrub canary timed out before SessionStart hook evidence")
		}
		t.Fatalf("claude subprocess scrub canary produced no hook evidence: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(report)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			got[key] = value
		}
	}
	for key, want := range map[string]string{
		"auth_token":  "absent",
		"api_key":     "absent",
		"base_url":    "present",
		"report_path": "present",
	} {
		if got[key] != want {
			t.Fatalf("subprocess env %s = %q, want %q", key, got[key], want)
		}
	}
}
