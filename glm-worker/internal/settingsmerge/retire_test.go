package settingsmerge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetireClaudeRestoresOnlyManagedSettings(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "fragment.json")
	override := filepath.Join(dir, "override.json")
	t.Setenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE", override)
	writeTestFile(t, target, `{"other":"keep","env":{"LOCAL":"old"}}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"set"}}`)
	writeTestFile(t, override, `{"env":{"LOCAL":"new"}}`)
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatal(err)
	}
	if err := RetireFiles(target); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"other"}, "keep")
	assertJSONValue(t, target, []string{"env", "LOCAL"}, "old")
	assertJSONMissing(t, target, []string{"env", "MANAGED"})
	for _, path := range []string{ManagedStatePath(target), statePathFor(target)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("ownership record remains %s: %v", path, err)
		}
	}
	if err := RetireFiles(target); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
}

func TestRetireClaudeRejectsChangedManagedValueAndKeepsState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "fragment.json")
	writeTestFile(t, target, `{"other":"keep"}`)
	writeTestFile(t, fragment, `{"env":{"MANAGED":"set"}}`)
	if _, err := MergeFiles(target, fragment, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, target, `{"env":{"MANAGED":"user"},"other":"keep"}`)
	if err := RetireFiles(target); err == nil || !strings.Contains(err.Error(), "user-modified") {
		t.Fatalf("expected refusal: %v", err)
	}
	if _, err := os.Stat(ManagedStatePath(target)); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "MANAGED"}, "user")
}

func TestRetireClaudeRejectsChangedOverrideAndKeepsState(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "fragment.json")
	override := filepath.Join(dir, "override.json")
	t.Setenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE", override)
	writeTestFile(t, target, `{"env":{"LOCAL":"original"}}`)
	writeTestFile(t, fragment, `{}`)
	writeTestFile(t, override, `{"env":{"LOCAL":"new"}}`)
	if _, err := MergeFiles(target, fragment, override); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, target, `{"env":{"LOCAL":"user"}}`)
	if err := RetireFiles(target); err == nil {
		t.Fatal("modified override accepted")
	}
	if _, err := os.Stat(statePathFor(target)); err != nil {
		t.Fatal(err)
	}
	assertJSONValue(t, target, []string{"env", "LOCAL"}, "user")
}
