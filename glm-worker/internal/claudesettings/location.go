package claudesettings

import (
	"fmt"
	"path/filepath"
)

type Location struct {
	ConfigDir    string
	SettingsPath string
}

func Resolve(home, configDir, settingsPath string) (Location, error) {
	configuredDir := configDir != ""
	configuredPath := settingsPath != ""

	if !configuredDir && !configuredPath {
		configDir = filepath.Join(home, ".claude")
		return Location{ConfigDir: configDir, SettingsPath: filepath.Join(configDir, "settings.json")}, nil
	}
	if configuredDir && !filepath.IsAbs(configDir) {
		return Location{}, fmt.Errorf("CLAUDE_CONFIG_DIR must be absolute: %s", configDir)
	}
	if configuredPath && !filepath.IsAbs(settingsPath) {
		return Location{}, fmt.Errorf("CLAUDE_SETTINGS_FILE must be absolute: %s", settingsPath)
	}
	if configuredDir && !configuredPath {
		configDir = filepath.Clean(configDir)
		return Location{ConfigDir: configDir, SettingsPath: filepath.Join(configDir, "settings.json")}, nil
	}

	settingsPath = filepath.Clean(settingsPath)
	if filepath.Base(settingsPath) != "settings.json" {
		return Location{}, fmt.Errorf("CLAUDE_SETTINGS_FILE must name settings.json: %s", settingsPath)
	}
	settingsDir := filepath.Dir(settingsPath)
	if !configuredDir {
		return Location{ConfigDir: settingsDir, SettingsPath: settingsPath}, nil
	}

	configDir = filepath.Clean(configDir)
	expected := filepath.Join(configDir, "settings.json")
	if settingsPath != expected {
		return Location{}, fmt.Errorf("CLAUDE_CONFIG_DIR and CLAUDE_SETTINGS_FILE resolve to different settings files: %s != %s", expected, settingsPath)
	}
	return Location{ConfigDir: configDir, SettingsPath: settingsPath}, nil
}
