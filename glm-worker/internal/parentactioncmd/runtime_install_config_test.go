package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestVerifyInstalledCodexManagedConfigPreservesLocalKeysButRejectsManagedDrift(t *testing.T) {
	repo := t.TempDir()
	codexDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "codex", "config-managed.toml"), []byte("background_terminal_max_timeout = 21600000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(installed, []byte("local_key = \"keep\"\nbackground_terminal_max_timeout = 21600000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, CodexConfigDir: codexDir}
	if err := verifyInstalledCodexManagedConfig(cfg); err != nil {
		t.Fatalf("matching managed config rejected: %v", err)
	}
	if err := os.WriteFile(installed, []byte("local_key = \"keep\"\nbackground_terminal_max_timeout = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledCodexManagedConfig(cfg); err == nil {
		t.Fatal("stale managed Codex value was accepted")
	}
}

func TestVerifyInstalledClaudeManagedSettingsAppliesLocalOverride(t *testing.T) {
	repo := t.TempDir()
	claudeDir := t.TempDir()
	overrideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	managed := `{"env":{"A":"managed","B":"managed"},"permissions":{"mode":"managed"}}`
	if err := os.WriteFile(filepath.Join(repo, "claude", "settings-managed.json"), []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "claude-settings.local.json")
	if err := os.WriteFile(overridePath, []byte(`{"env":{"A":"override","B":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(claudeDir, "settings.json")
	installed := `{"env":{"A":"override","LOCAL":"keep"},"permissions":{"mode":"managed","local":true},"other":"keep"}`
	if err := os.WriteFile(installedPath, []byte(installed), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, ClaudeConfigDir: claudeDir, ClaudeSettingsOverride: overridePath}
	if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
		t.Fatalf("matching managed Claude settings rejected: %v", err)
	}
	stale := `{"env":{"A":"managed","LOCAL":"keep"},"permissions":{"mode":"managed","local":true}}`
	if err := os.WriteFile(installedPath, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("stale managed Claude value was accepted")
	}
}

func TestVerifyRuntimeMergedConfigFilesOnlyChecksChangedManagedSurfaces(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir()}
	if err := verifyRuntimeMergedConfigFiles(cfg, []string{"glm-worker/internal/app/app.go"}); err != nil {
		t.Fatalf("unrelated runtime path required merged config state: %v", err)
	}
}
