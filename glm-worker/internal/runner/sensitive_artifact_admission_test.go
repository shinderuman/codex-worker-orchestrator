package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestSensitiveArtifactAdmissionRejectsEffectiveProviderAuthToken(t *testing.T) {
	const secret = "provider-auth-secret-395"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	if err := os.MkdirAll(cfg.ClaudeConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"`+secret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	providerValues := sensitiveArtifactProviderValues(t, cfg)
	if err := os.WriteFile(artifact, []byte("response header: "+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact), providerValues)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAuthToken {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("diagnostic leaked secret: %v", err)
	}
}

func TestSensitiveArtifactAdmissionRejectsAllowlistedProviderAPIKey(t *testing.T) {
	const secret = "provider-api-secret-395"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	cfg.EnvAllowlist = []string{"ANTHROPIC_API_KEY"}
	t.Setenv("ANTHROPIC_API_KEY", secret)
	providerValues := sensitiveArtifactProviderValues(t, cfg)
	if err := os.WriteFile(artifact, []byte("payload="+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact), providerValues)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAPIKey {
		t.Fatalf("error = %v", err)
	}
}

func TestSensitiveArtifactAdmissionIgnoresUnforwardedParentAPIKey(t *testing.T) {
	const secret = "parent-only-api-secret-395"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	t.Setenv("ANTHROPIC_API_KEY", secret)
	providerValues := sensitiveArtifactProviderValues(t, cfg)
	if err := os.WriteFile(artifact, []byte("payload="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact), providerValues); err != nil {
		t.Fatalf("unforwarded parent env was treated as a runtime credential: %v", err)
	}
}

func TestSensitiveArtifactAdmissionRejectsLiveParentActionToken(t *testing.T) {
	cfg, st, artifact := sensitiveArtifactTestState(t)
	prepared, err := parentaction.Prepare(cfg.RepoRoot, string(parentaction.ActionDecision))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("token="+prepared.Token), 0o600); err != nil {
		t.Fatal(err)
	}

	err = validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact), nil)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != "parent-action-token" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), prepared.Token) {
		t.Fatalf("diagnostic leaked parent action token: %v", err)
	}
}

func TestSensitiveArtifactAdmissionRemovesEveryRejectedArtifact(t *testing.T) {
	const secret = "provider-auth-secret-all-395"
	cfg, st, first := sensitiveArtifactTestState(t)
	second := filepath.Join(filepath.Dir(first), "second-evidence.txt")
	values := []SensitiveArtifactValue{{Category: SensitiveArtifactProviderAuthToken, Value: secret}}
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("secret="+secret), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, first, second), values)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) {
		t.Fatalf("error = %v", err)
	}
	for _, path := range []string{first, second} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("sensitive artifact remained at %s: %v", path, statErr)
		}
	}
}

func TestSensitiveArtifactAdmissionScansSafeArtifactsFromInvalidList(t *testing.T) {
	const secret = "provider-auth-secret-invalid-list-395"
	values := []SensitiveArtifactValue{{Category: SensitiveArtifactProviderAuthToken, Value: secret}}
	for _, tc := range []struct {
		name      string
		artifacts func(string) []any
	}{
		{
			name: "duplicate",
			artifacts: func(path string) []any {
				return []any{path, path}
			},
		},
		{
			name: "mixed invalid path",
			artifacts: func(path string) []any {
				return []any{path, "relative-outside.txt"}
			},
		},
		{
			name: "mixed invalid type",
			artifacts: func(path string) []any {
				return []any{path, 7}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st, artifact := sensitiveArtifactTestState(t)
			if err := os.WriteFile(artifact, []byte("secret="+secret), 0o600); err != nil {
				t.Fatal(err)
			}
			structured, err := json.Marshal(map[string]any{
				"status":    "IMPLEMENTED",
				"risk":      "LOW",
				"artifacts": tc.artifacts(artifact),
			})
			if err != nil {
				t.Fatal(err)
			}
			err = validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, RunResult{StructuredOutput: structured}, values)
			var sensitive *SensitiveArtifactError
			if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAuthToken {
				t.Fatalf("error = %v", err)
			}
			if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("sensitive artifact remained after invalid list: %v", statErr)
			}
		})
	}
}

func TestSensitiveArtifactAdmissionScansArtifactsFromOtherwiseInvalidPacket(t *testing.T) {
	const secret = "provider-auth-secret-invalid-packet-395"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	if err := os.WriteFile(artifact, []byte("secret="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	structured, err := json.Marshal(map[string]any{"artifacts": []string{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	err = validateSensitiveResultArtifacts(
		&ClaudeRunner{config: cfg, state: st},
		RunResult{StructuredOutput: structured},
		[]SensitiveArtifactValue{{Category: SensitiveArtifactProviderAuthToken, Value: secret}},
	)
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sensitive artifact remained after invalid packet: %v", statErr)
	}
}

func TestSensitiveArtifactAdmissionAcceptsSanitizedArtifact(t *testing.T) {
	cfg, st, artifact := sensitiveArtifactTestState(t)
	if err := os.MkdirAll(cfg.ClaudeConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"secret-not-present"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	providerValues := sensitiveArtifactProviderValues(t, cfg)
	if err := os.WriteFile(artifact, []byte("status=502; authorization=[REDACTED]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact), providerValues); err != nil {
		t.Fatalf("sanitized artifact rejected: %v", err)
	}
}

func sensitiveArtifactProviderValues(t *testing.T, cfg config.AppConfig) []SensitiveArtifactValue {
	t.Helper()
	values, err := SensitiveArtifactValues(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func sensitiveArtifactTestState(t *testing.T) (config.AppConfig, *state.StateStore, string) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:        repoRoot,
		RepoHash:        "sensitive-artifact-test",
		StateBase:       t.TempDir(),
		ClaudeConfigDir: t.TempDir(),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	artifactDir, err := st.PrepareArtifactDir()
	if err != nil {
		t.Fatal(err)
	}
	if artifactDir != st.ArtifactDir(taskID) {
		t.Fatalf("artifact dir = %q want %q", artifactDir, st.ArtifactDir(taskID))
	}
	return cfg, st, filepath.Join(artifactDir, "failure-evidence.txt")
}

func sensitiveArtifactResult(t *testing.T, artifacts ...string) RunResult {
	t.Helper()
	structured, err := json.Marshal(map[string]any{
		"status":               "IMPLEMENTED",
		"risk":                 "LOW",
		"summary":              "done",
		"requirement_coverage": "covered",
		"tests":                "pass",
		"unverified":           "none",
		"artifacts":            artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return RunResult{StructuredOutput: structured}
}
