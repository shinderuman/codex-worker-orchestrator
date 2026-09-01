package claudeoverride

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// EnvOverride is the canonical decoded form of claude-settings.local.json.
type EnvOverride struct {
	Sets    map[string]string
	Deletes []string
}

// ResolvePath applies the canonical local-override path precedence for a known home directory.
func ResolvePath(home string) string {
	if value := os.Getenv("CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE"); value != "" {
		return value
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "codex-config", "claude-settings.local.json")
}

// Load reads and validates the optional local override file.
func Load(path string) (EnvOverride, error) {
	if path == "" {
		return EnvOverride{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return EnvOverride{}, nil
	}
	if err != nil {
		return EnvOverride{}, err
	}
	return Decode(data)
}

// Decode validates the persisted local-override wire format.
func Decode(data []byte) (EnvOverride, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return EnvOverride{}, fmt.Errorf("override JSON: %w", err)
	}
	raw, ok := value.(map[string]any)
	if !ok {
		if value == nil {
			return EnvOverride{}, fmt.Errorf("override JSON: top-level nullは許可されません")
		}
		return EnvOverride{}, fmt.Errorf("override JSON: top-levelはobjectのみ許可されます")
	}
	for key := range raw {
		if key != "env" {
			return EnvOverride{}, fmt.Errorf("top-level key %qは許可されません (envのみ)", key)
		}
	}
	envValue, ok := raw["env"]
	if !ok {
		return EnvOverride{}, nil
	}
	if envValue == nil {
		return EnvOverride{}, fmt.Errorf("override env: nullは許可されません (objectまたは空object)")
	}
	entries, ok := envValue.(map[string]any)
	if !ok {
		return EnvOverride{}, fmt.Errorf("override env: objectのみ許可されます")
	}
	override := EnvOverride{Sets: make(map[string]string, len(entries))}
	for key, value := range entries {
		switch typed := value.(type) {
		case string:
			override.Sets[key] = typed
		case nil:
			override.Deletes = append(override.Deletes, key)
		default:
			return EnvOverride{}, fmt.Errorf("override env %qはstringかnullのみ許可されます", key)
		}
	}
	return override, nil
}
