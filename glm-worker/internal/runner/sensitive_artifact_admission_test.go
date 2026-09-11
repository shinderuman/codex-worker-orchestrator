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
	if err := os.WriteFile(artifact, []byte("response header: "+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact))
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
	if err := os.WriteFile(artifact, []byte("payload="+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact))
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAPIKey {
		t.Fatalf("error = %v", err)
	}
}

func TestSensitiveArtifactAdmissionIgnoresUnforwardedParentAPIKey(t *testing.T) {
	const secret = "parent-only-api-secret-395"
	cfg, st, artifact := sensitiveArtifactTestState(t)
	t.Setenv("ANTHROPIC_API_KEY", secret)
	if err := os.WriteFile(artifact, []byte("payload="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact)); err != nil {
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

	err = validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact))
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != "parent-action-token" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), prepared.Token) {
		t.Fatalf("diagnostic leaked parent action token: %v", err)
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
	if err := os.WriteFile(artifact, []byte("status=502; authorization=[REDACTED]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateSensitiveResultArtifacts(&ClaudeRunner{config: cfg, state: st}, sensitiveArtifactResult(t, artifact)); err != nil {
		t.Fatalf("sanitized artifact rejected: %v", err)
	}
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

func sensitiveArtifactResult(t *testing.T, artifact string) RunResult {
	t.Helper()
	structured, err := json.Marshal(map[string]any{"artifacts": []string{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	return RunResult{StructuredOutput: structured}
}
