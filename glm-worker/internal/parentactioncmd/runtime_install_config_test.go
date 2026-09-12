package parentactioncmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func TestVerifyInstalledClaudeManagedSettingsAppliesLocalOverride(t *testing.T) {
	repo := t.TempDir()
	claudeDir := t.TempDir()
	overrideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedPath := filepath.Join(repo, "claude", "settings-managed.json")
	managed := `{"env":{"A":"managed","B":"managed"},"permissions":{"mode":"managed"}}`
	if err := os.WriteFile(managedPath, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "claude-settings.local.json")
	if err := os.WriteFile(overridePath, []byte(`{"env":{"A":"override","B":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(claudeDir, "settings.json")
	baseline := `{"env":{"LOCAL":"keep"},"permissions":{"local":true},"other":"keep"}`
	if err := os.WriteFile(installedPath, []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settingsmerge.MergeFiles(installedPath, managedPath, overridePath); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, ClaudeConfigDir: claudeDir, ClaudeSettingsPath: installedPath, ClaudeSettingsOverride: overridePath}
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

func TestVerifyInstalledClaudeManagedSettingsChecksOverrideOnlyEnvKeys(t *testing.T) {
	repo := t.TempDir()
	claudeDir := t.TempDir()
	overrideDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	managedPath := filepath.Join(repo, "claude", "settings-managed.json")
	managed := `{"env":{"A":"managed"}}`
	if err := os.WriteFile(managedPath, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(overrideDir, "claude-settings.local.json")
	if err := os.WriteFile(overridePath, []byte(`{"env":{"ONLY":"override","REMOVE":null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(installedPath, []byte(`{"env":{"REMOVE":"baseline"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settingsmerge.MergeFiles(installedPath, managedPath, overridePath); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{RepoRoot: repo, ClaudeConfigDir: claudeDir, ClaudeSettingsPath: installedPath, ClaudeSettingsOverride: overridePath}

	if err := verifyInstalledClaudeManagedSettings(cfg); err != nil {
		t.Fatalf("valid override-only env state rejected: %v", err)
	}
	if err := os.WriteFile(installedPath, []byte(`{"env":{"A":"managed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("missing override-only set key was accepted")
	}
	if err := os.WriteFile(installedPath, []byte(`{"env":{"A":"managed","ONLY":"override","REMOVE":"stale"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInstalledClaudeManagedSettings(cfg); err == nil {
		t.Fatal("retained override-only delete key was accepted")
	}
}
