package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestInstructionSurfaceRunnerRejectsCredentialArtifactBeforeSuccess(t *testing.T) {
	const secret = "production-provider-secret-395"
	root := newGitAuthorityRepo(t)
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	claudeConfigDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"`+secret+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{
		RepoRoot:        root,
		RepoHash:        "sensitive-artifact-production",
		RepoShort:       "sensitive395",
		StateBase:       t.TempDir(),
		PromptDir:       promptDir,
		ClaudeConfigDir: claudeConfigDir,
		EnvAllowlist:    []string{"TEST_ARTIFACT", "TEST_RESULT_JSON"},
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	artifactDir, err := st.PrepareArtifactDir()
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactDir, "failure-evidence.txt")
	resultJSON, err := json.Marshal(map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": false,
		"structured_output": map[string]any{
			"status":               "IMPLEMENTED",
			"risk":                 "LOW",
			"summary":              "done",
			"requirement_coverage": "covered",
			"tests":                "pass",
			"unverified":           "none",
			"artifacts":            []string{artifact},
		},
		"result": "runner output",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ARTIFACT", artifact)
	t.Setenv("TEST_RESULT_JSON", string(resultJSON))
	claudeBin := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\nprintf '%s' \"$ANTHROPIC_AUTH_TOKEN\" >\"$TEST_ARTIFACT\"\nprintf '%s\\n' \"$TEST_RESULT_JSON\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.ClaudeBin = claudeBin
	guarded := NewInstructionSurfaceGuardRunner(NewClaudeRunner(cfg, st))

	_, err = guarded.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	var sensitive *SensitiveArtifactError
	if !errors.As(err, &sensitive) || sensitive.Category != SensitiveArtifactProviderAuthToken {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(artifact); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected artifact remained: %v", statErr)
	}
	if st.Exists("worker.id") || st.Exists("worker.ready") {
		t.Fatal("sensitive artifact rejection left a reusable worker session")
	}
}
