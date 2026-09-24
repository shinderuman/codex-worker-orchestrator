package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	for _, tc := range []struct {
		name          string
		credentialKey string
	}{
		{name: "auth-token", credentialKey: "ANTHROPIC_AUTH_TOKEN"},
		{name: "api-key", credentialKey: "ANTHROPIC_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runClaudeSubprocessEnvScrubCanary(t, resolved, tc.credentialKey)
		})
	}
}

func runClaudeSubprocessEnvScrubCanary(t *testing.T, claudeBin, credentialKey string) {
	t.Helper()
	home := t.TempDir()
	reportPath := filepath.Join(home, "subprocess-env-report")
	credentialValue := "glm-worker-subprocess-scrub-canary"
	bashCommand := `auth_token=absent; api_key=absent; ` +
		`[ -n "${ANTHROPIC_AUTH_TOKEN:-}" ] && auth_token=present; ` +
		`[ -n "${ANTHROPIC_API_KEY:-}" ] && api_key=present; ` +
		`printf 'auth_token=%s\napi_key=%s\n' "$auth_token" "$api_key" > "$GLM_SUBPROCESS_SCRUB_REPORT"`

	var requestCount atomic.Int32
	var parentCredentialSeen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isClaudeProviderHealthCheck(r) {
			w.WriteHeader(http.StatusOK)
			return
		}
		if requestHasCredential(r, credentialValue) {
			parentCredentialSeen.Store(true)
		}
		requestNumber := requestCount.Add(1)
		writeClaudeCanaryResponse(t, w, requestNumber, bashCommand)
	}))
	defer server.Close()

	configDir := prepareClaudeCanaryHome(t, home)
	settings, err := isolationSettings(configDir, &gitBashSandboxPolicy{allowWrite: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	settingEnv := map[string]string{credentialKey: credentialValue}
	settingEnv["ANTHROPIC_BASE_URL"] = server.URL + "/api/anthropic"
	settingEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "glm-canary"
	settingEnv["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	additions := claudeInvocationEnvDefaults()
	additions["HOME"] = home
	additions["CLAUDE_CONFIG_DIR"] = configDir
	additions["GLM_SUBPROCESS_SCRUB_REPORT"] = reportPath

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, claudeBin,
		"-p", "--safe-mode", "--setting-sources", "", "--no-session-persistence",
		"--model", "opus", "--output-format", "json", "--dangerously-skip-permissions",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--disable-slash-commands",
		"--tools", "Bash", "--settings", settings, "run the provided Bash tool",
	)
	command.Dir = "."
	command.Env = buildChildEnv(nil, settingEnv, additions, nil)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	runErr := command.Run()
	if ctx.Err() != nil {
		t.Fatal("claude subprocess scrub canary timed out")
	}
	if bytes.Contains(output.Bytes(), []byte("Permission mode forced to default")) {
		t.Fatal("subprocess scrub changed the configured bypass permission mode")
	}
	if runErr != nil {
		t.Fatalf("claude subprocess scrub canary failed: type=%T requests=%d parent_credential=%t category=%s", runErr, requestCount.Load(), parentCredentialSeen.Load(), claudeCanaryFailureCategory(output.Bytes()))
	}
	if !parentCredentialSeen.Load() {
		t.Fatal("configured provider credential was not observed on the parent Claude provider request")
	}
	if requestCount.Load() < 2 {
		t.Fatalf("provider request count = %d, want at least 2", requestCount.Load())
	}
	assertScrubCanaryReport(t, reportPath)
}

func claudeCanaryFailureCategory(output []byte) string {
	for _, candidate := range []struct {
		needle   string
		category string
	}{
		{needle: "Not logged in", category: "not-logged-in"},
		{needle: "Invalid API key", category: "invalid-api-key"},
		{needle: "Connection refused", category: "connection-refused"},
		{needle: "ECONNREFUSED", category: "connection-refused"},
		{needle: "API Error", category: "api-error"},
		{needle: "tool_use", category: "tool-use-protocol"},
		{needle: "safe mode", category: "safe-mode"},
		{needle: "bubblewrap is required", category: "bubblewrap-missing"},
	} {
		if bytes.Contains(output, []byte(candidate.needle)) {
			return candidate.category
		}
	}
	return "unclassified"
}

func requestHasCredential(r *http.Request, credential string) bool {
	for _, values := range r.Header {
		for _, value := range values {
			if strings.Contains(value, credential) {
				return true
			}
		}
	}
	return false
}

func writeClaudeCanaryResponse(t *testing.T, w http.ResponseWriter, requestNumber int32, bashCommand string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	if requestNumber == 1 {
		writeClaudeToolUseStream(t, w, bashCommand)
		return
	}
	writeClaudeTextStream(t, w)
}

func writeClaudeToolUseStream(t *testing.T, w http.ResponseWriter, bashCommand string) {
	t.Helper()
	writeClaudeSSE(t, w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_canary_tool", "type": "message", "role": "assistant", "content": []any{},
			"model": "glm-canary", "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
		},
	})
	writeClaudeSSE(t, w, "content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": "toolu_canary", "name": "Bash", "input": map[string]any{}},
	})
	input, err := json.Marshal(map[string]string{"command": bashCommand})
	if err != nil {
		t.Fatal(err)
	}
	writeClaudeSSE(t, w, "content_block_delta", map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)},
	})
	writeClaudeSSE(t, w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeSSE(t, w, "message_delta", map[string]any{
		"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 1},
	})
	writeClaudeSSE(t, w, "message_stop", map[string]any{"type": "message_stop"})
}

func writeClaudeTextStream(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeClaudeSSE(t, w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg_canary_done", "type": "message", "role": "assistant", "content": []any{},
			"model": "glm-canary", "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
		},
	})
	writeClaudeSSE(t, w, "content_block_start", map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	writeClaudeSSE(t, w, "content_block_delta", map[string]any{
		"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": "ok"},
	})
	writeClaudeSSE(t, w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeClaudeSSE(t, w, "message_delta", map[string]any{
		"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 1},
	})
	writeClaudeSSE(t, w, "message_stop", map[string]any{"type": "message_stop"})
}

func writeClaudeSSE(t *testing.T, w http.ResponseWriter, event string, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func assertScrubCanaryReport(t *testing.T, path string) {
	t.Helper()
	report, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Bash subprocess produced no scrub evidence: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(report)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			got[key] = value
		}
	}
	for _, key := range []string{"auth_token", "api_key"} {
		if got[key] != "absent" {
			t.Fatalf("Bash subprocess credential category %s = %q, want absent", key, got[key])
		}
	}
}
