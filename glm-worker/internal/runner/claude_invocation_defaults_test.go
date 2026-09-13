package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestClaudeInvocationDefaultsPreserveConfiguredValues(t *testing.T) {
	defaults := claudeInvocationEnvDefaults()
	if got, want := defaults["CLAUDE_CODE_AUTO_COMPACT_WINDOW"], strconv.Itoa(configuredAutoCompactWindowTokens); got != want {
		t.Fatalf("auto compact env = %q, want %q", got, want)
	}
	if got := defaults["CLAUDE_CODE_ALWAYS_ENABLE_EFFORT"]; got != configuredAlwaysEnableEffort {
		t.Fatalf("always effort env = %q, want %q", got, configuredAlwaysEnableEffort)
	}
	if configuredAutoCompactWindowTokens != 500_000 {
		t.Fatalf("configured auto compact tokens = %d, want 500000", configuredAutoCompactWindowTokens)
	}
	if configuredAlwaysEnableEffort != "1" {
		t.Fatalf("configured always effort = %q, want 1", configuredAlwaysEnableEffort)
	}
}

func TestAutoCompactArgumentDerivesFromConfiguredTokens(t *testing.T) {
	if got := configuredAutoCompactWindowArgument; got != "500k" {
		t.Fatalf("configured auto compact argument = %q, want 500k", got)
	}
	if got := formatAutoCompactWindowArgument(500_001); got != "500001" {
		t.Fatalf("non-thousand argument = %q, want 500001", got)
	}
}

func TestManagedClaudeSettingsMirrorOrchestratorInvocationDefaults(t *testing.T) {
	path := filepath.Join("..", "..", "..", "claude", "settings-managed.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for key, want := range claudeInvocationEnvDefaults() {
		if got := settings.Env[key]; got != want {
			t.Fatalf("managed Claude projection %s = %q, want %q", key, got, want)
		}
	}
}

func TestIsolationSmokeDoesNotOwnClaudeInvocationDefaults(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "isolation-smoke.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, forbidden := range []string{
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT",
		"--autocompact",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("isolation smoke must not own invocation default %q", forbidden)
		}
	}
}
