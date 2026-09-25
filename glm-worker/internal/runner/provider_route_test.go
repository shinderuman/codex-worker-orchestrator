package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func managedProviderConfig(t *testing.T, env map[string]any) config.AppConfig {
	t.Helper()
	claudeDir := t.TempDir()
	if env != nil {
		writeSettings(t, claudeDir, env)
	}
	return config.AppConfig{
		ClaudeConfigDir:    claudeDir,
		ClaudeSettingsPath: filepath.Join(claudeDir, "settings.json"),
	}
}

func TestManagedProviderRouteAllowsExplicitCompatibleRoutes(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.z.ai/api/anthropic",
		"https://compatible.example/anthropic",
	} {
		t.Run(baseURL, func(t *testing.T) {
			cfg := managedProviderConfig(t, map[string]any{anthropicBaseURLEnv: baseURL})
			env, _, err := loadConfiguredSettingEnv(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := env[anthropicBaseURLEnv]; got != baseURL {
				t.Fatalf("%s = %q want %q", anthropicBaseURLEnv, got, baseURL)
			}
		})
	}
}

func TestManagedProviderRouteRejectsDefaultAnthropicExposure(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]any
		want string
	}{
		{name: "missing settings", env: nil, want: "fallbackを拒否"},
		{name: "missing base url", env: map[string]any{"API_TIMEOUT_MS": "3000000"}, want: "fallbackを拒否"},
		{name: "anthropic host", env: map[string]any{anthropicBaseURLEnv: "https://api.anthropic.com"}, want: "unsupported Anthropic provider"},
		{name: "anthropic subdomain", env: map[string]any{anthropicBaseURLEnv: "https://proxy.anthropic.com/v1"}, want: "unsupported Anthropic provider"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := managedProviderConfig(t, tc.env)
			_, _, err := loadConfiguredSettingEnv(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v want substring %q", err, tc.want)
			}
		})
	}
}

func TestManagedProviderRouteTombstoneCannotReflowFromParent(t *testing.T) {
	cfg := managedProviderConfig(t, map[string]any{anthropicBaseURLEnv: "https://api.z.ai/api/anthropic"})
	overrideBody, err := json.Marshal(map[string]any{"env": map[string]any{anthropicBaseURLEnv: nil}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClaudeSettingsOverride = writeOverrideFile(t, string(overrideBody))
	cfg.EnvAllowlist = []string{anthropicBaseURLEnv}
	t.Setenv(anthropicBaseURLEnv, "https://api.z.ai/api/anthropic")

	_, _, err = loadConfiguredSettingEnv(cfg)
	if err == nil || !strings.Contains(err.Error(), "fallbackを拒否") {
		t.Fatalf("tombstone must fail closed instead of reflowing parent route: %v", err)
	}
}

func TestManagedProviderRouteAllowsExplicitParentRouteWhenAllowlisted(t *testing.T) {
	cfg := managedProviderConfig(t, nil)
	cfg.EnvAllowlist = []string{anthropicBaseURLEnv}
	t.Setenv(anthropicBaseURLEnv, "https://compatible.example/anthropic")

	if _, _, err := loadConfiguredSettingEnv(cfg); err != nil {
		t.Fatalf("explicit allowlisted compatible parent route must remain usable: %v", err)
	}
}

func TestManagedProviderRouteGuardCoversRunDecideAndProbe(t *testing.T) {
	promptDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptDir, "WORKER.md"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	cfg := managedProviderConfig(t, nil)
	cfg.RepoRoot = t.TempDir()
	cfg.RepoShort = "provider-route"
	cfg.PromptDir = promptDir
	cfg.ClaudeBin = filepath.Join(t.TempDir(), "must-not-run")
	r := NewClaudeRunner(cfg, st)

	assertRouteGuard := func(name string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), anthropicBaseURLEnv) {
			t.Fatalf("%s must fail at provider route guard, got %v", name, err)
		}
	}

	_, err := r.Run(state.WorkerRole, "provider-route", "opus", false, "high", "prompt", filepath.Join(t.TempDir(), "run.out"))
	assertRouteGuard("Run", err)

	_, err = r.Decide("opus", "low", "{}", "prompt")
	assertRouteGuard("Decide", err)

	_, err = r.Probe("opus")
	assertRouteGuard("Probe", err)
}

func TestSensitiveArtifactValuesDoesNotRequireProviderRoute(t *testing.T) {
	cfg := managedProviderConfig(t, nil)
	cfg.EnvAllowlist = []string{"ANTHROPIC_AUTH_TOKEN"}
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "parent-token")

	values, err := SensitiveArtifactValues(cfg)
	if err != nil {
		t.Fatalf("artifact scanning must remain outside model-call provider gate: %v", err)
	}
	if len(values) != 1 || values[0].Category != SensitiveArtifactProviderAuthToken || values[0].Value != "parent-token" {
		t.Fatalf("values = %#v", values)
	}
}
