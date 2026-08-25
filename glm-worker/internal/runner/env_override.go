package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type claudeEnvOverride struct {
	sets    map[string]string
	deletes []string
}

func parseClaudeEnvOverride(path string) (claudeEnvOverride, error) {
	var override claudeEnvOverride
	if path == "" {
		return override, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return override, nil
		}
		return override, err
	}

	var topLevel any
	if err := json.Unmarshal(data, &topLevel); err != nil {
		return override, fmt.Errorf("override JSON: %w", err)
	}
	if topLevel == nil {
		return override, fmt.Errorf("override JSON: top-level nullは許可されません")
	}
	raw, ok := topLevel.(map[string]any)
	if !ok {
		return override, fmt.Errorf("override JSON: top-levelはobjectのみ許可されます")
	}
	for key := range raw {
		if key != "env" {
			return override, fmt.Errorf("top-level key %qは許可されません (envのみ)", key)
		}
	}

	envValue, hasEnv := raw["env"]
	if !hasEnv {
		return override, nil
	}
	if envValue == nil {
		return override, fmt.Errorf("override env: nullは許可されません (objectまたは空object)")
	}
	entries, ok := envValue.(map[string]any)
	if !ok {
		return override, fmt.Errorf("override env: objectのみ許可されます")
	}

	override.sets = make(map[string]string, len(entries))
	for key, value := range entries {
		switch typed := value.(type) {
		case string:
			override.sets[key] = typed
		case nil:
			override.deletes = append(override.deletes, key)
		default:
			return override, fmt.Errorf("override env %qはstringかnullのみ許可されます", key)
		}
	}
	return override, nil
}
