package mergejsoncmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	if err := os.WriteFile(fragment, []byte(`{"env":{"A":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"-target", target, "-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "updated\n" {
		t.Fatalf("output=%q", output.String())
	}
	output.Reset()
	if err := Run([]string{"-target", target, "-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "unchanged\n" {
		t.Fatalf("output=%q", output.String())
	}
}

func TestRunUsesCanonicalClaudeSettingsLocationWhenTargetIsOmitted(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	settingsPath := filepath.Join(dir, "custom-claude", "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SETTINGS_FILE", settingsPath)
	t.Setenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE", "")
	if err := os.WriteFile(fragment, []byte(`{"env":{"A":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Run([]string{"-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "updated\n" {
		t.Fatalf("output=%q", output.String())
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"A": "1"`) {
		t.Fatalf("managed setting missing from %s: %s", settingsPath, data)
	}
}

func TestRunRejectsConflictingClaudeSettingsLocation(t *testing.T) {
	dir := t.TempDir()
	fragment := filepath.Join(dir, "managed.json")
	if err := os.WriteFile(fragment, []byte(`{"env":{"A":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "one"))
	t.Setenv("CLAUDE_SETTINGS_FILE", filepath.Join(dir, "two", "settings.json"))

	err := Run([]string{"-fragment", fragment}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "different settings files") {
		t.Fatalf("expected location conflict, got %v", err)
	}
}

func TestRunUsesCanonicalDefaultOverridePath(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE", "")

	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	overrideDir := filepath.Join(xdg, "codex-config")
	if err := os.MkdirAll(overrideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"env":{"B":"local"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragment, []byte(`{"env":{"A":"managed","B":"managed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideDir, "claude-settings.local.json"), []byte(`{"env":{"A":"override","B":null,"C":"added"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Run([]string{"-target", target, "-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"A": "override"`, `"C": "added"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	if strings.Contains(text, `"B"`) {
		t.Fatalf("null override must remove B: %s", text)
	}
}

func TestRunRejectsMissingPaths(t *testing.T) {
	if err := Run(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("expected usage error")
	}
}
