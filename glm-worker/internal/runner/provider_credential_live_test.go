package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type providerCredentialRequestObservation struct {
	method              string
	path                string
	authorizationHeader bool
	apiKeyHeader        bool
	expectedCredential  bool
}

func TestClaudeProviderCredentialLiveNoAI(t *testing.T) {
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
			credential := "glm-worker-provider-canary"
			var mu sync.Mutex
			var observation providerCredentialRequestObservation
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				observation = providerCredentialRequestObservation{
					method:              r.Method,
					path:                r.URL.Path,
					authorizationHeader: r.Header.Get("Authorization") != "",
					apiKeyHeader:        r.Header.Get("X-Api-Key") != "",
					expectedCredential:  requestHeaderContainsCredential(r, tc.credentialKey, credential),
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"type": "error",
					"error": map[string]string{"type": "authentication_error", "message": "canary stop"},
				})
			}))
			defer server.Close()

			dir := t.TempDir()
			configDir := filepath.Join(dir, "claude-config")
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				t.Fatal(err)
			}
			settingEnv := map[string]string{tc.credentialKey: credential}
			settingEnv["ANTHROPIC_BASE_URL"] = server.URL + "/api/anthropic"
			settingEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "glm-canary"
			settingEnv["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
			additions := claudeInvocationEnvDefaults()
			additions["CLAUDE_CONFIG_DIR"] = configDir

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, resolved,
				"-p", "--setting-sources", "", "--no-session-persistence",
				"--model", "opus", "--output-format", "json", "--disable-slash-commands",
				"--tools", "", "reply with ok",
			)
			command.Dir = "."
			command.Env = buildChildEnv(nil, settingEnv, additions, nil)
			var output bytes.Buffer
			command.Stdout = &output
			command.Stderr = &output
			runErr := command.Run()

			mu.Lock()
			got := observation
			mu.Unlock()
			if got.method == "" {
				t.Fatalf("Claude Code did not reach the configured provider endpoint: run_error=%T output=%q", runErr, sanitizedClaudeCanaryOutput(output.Bytes(), credential))
			}
			if !got.expectedCredential {
				t.Fatalf("provider credential missing: method=%s path=%s authorization_header=%t api_key_header=%t", got.method, got.path, got.authorizationHeader, got.apiKeyHeader)
			}
		})
	}
}

func sanitizedClaudeCanaryOutput(output []byte, credential string) string {
	const max = 2000
	text := strings.ReplaceAll(string(output), credential, "[REDACTED]")
	if len(text) > max {
		return text[:max] + "..."
	}
	return text
}

func requestHeaderContainsCredential(r *http.Request, key, credential string) bool {
	switch key {
	case "ANTHROPIC_AUTH_TOKEN":
		return strings.Contains(r.Header.Get("Authorization"), credential)
	case "ANTHROPIC_API_KEY":
		return r.Header.Get("X-Api-Key") == credential
	default:
		return false
	}
}
