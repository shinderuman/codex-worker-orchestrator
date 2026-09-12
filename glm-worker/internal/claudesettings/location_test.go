package claudesettings

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDefaultLocation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	location, err := Resolve(home, "", "")
	if err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(home, ".claude")
	if location.ConfigDir != wantDir || location.SettingsPath != filepath.Join(wantDir, "settings.json") {
		t.Fatalf("location=%+v", location)
	}
}

func TestResolveConfigDirLocation(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude")
	location, err := Resolve(t.TempDir(), configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if location.ConfigDir != configDir || location.SettingsPath != filepath.Join(configDir, "settings.json") {
		t.Fatalf("location=%+v", location)
	}
}

func TestResolveSettingsFileDerivesConfigDir(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "custom", "claude-settings.json")
	location, err := Resolve(t.TempDir(), "", settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if location.ConfigDir != filepath.Dir(settingsPath) || location.SettingsPath != settingsPath {
		t.Fatalf("location=%+v", location)
	}
}

func TestResolveAcceptsConsistentInputs(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "claude")
	settingsPath := filepath.Join(configDir, "settings.json")
	location, err := Resolve(t.TempDir(), configDir, settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if location.ConfigDir != configDir || location.SettingsPath != settingsPath {
		t.Fatalf("location=%+v", location)
	}
}

func TestResolveRejectsConflictingInputs(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "one")
	settingsPath := filepath.Join(t.TempDir(), "two", "settings.json")
	_, err := Resolve(t.TempDir(), configDir, settingsPath)
	if err == nil || !strings.Contains(err.Error(), "different settings files") {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestResolveRejectsRelativeLocations(t *testing.T) {
	for _, test := range []struct {
		name         string
		configDir    string
		settingsPath string
		want         string
	}{
		{name: "config dir", configDir: "relative-claude", want: "CLAUDE_CONFIG_DIR must be absolute"},
		{name: "settings file", settingsPath: filepath.Join("relative-claude", "settings.json"), want: "CLAUDE_SETTINGS_FILE must be absolute"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(t.TempDir(), test.configDir, test.settingsPath)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}
