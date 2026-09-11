package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDerivesClaudeConfigDirFromSettingsFile(t *testing.T) {
	repository := initConfigTestRepository(t)
	withConfigTestWorkingDirectory(t, repository)
	home := t.TempDir()
	settingsPath := filepath.Join(t.TempDir(), "custom-claude", "settings.json")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SETTINGS_FILE", settingsPath)

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClaudeSettingsPath != settingsPath || loaded.ClaudeConfigDir != filepath.Dir(settingsPath) {
		t.Fatalf("Claude location = dir %q settings %q", loaded.ClaudeConfigDir, loaded.ClaudeSettingsPath)
	}
}

func TestLoadRejectsConflictingClaudeLocationInputs(t *testing.T) {
	repository := initConfigTestRepository(t)
	withConfigTestWorkingDirectory(t, repository)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("CLAUDE_SETTINGS_FILE", filepath.Join(t.TempDir(), "other", "settings.json"))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "different settings files") {
		t.Fatalf("expected Claude location conflict, got %v", err)
	}
}

func initConfigTestRepository(t *testing.T) string {
	t.Helper()
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "init", "--quiet", repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return repository
}

func withConfigTestWorkingDirectory(t *testing.T, repository string) {
	t.Helper()
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repository); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })
}
