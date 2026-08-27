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
	data, found, err := readClaudeEnvOverride(path)
	if err != nil || !found {
		return claudeEnvOverride{}, err
	}
	entries, err := decodeClaudeEnvEntries(data)
	if err != nil {
		return claudeEnvOverride{}, err
	}
	return buildClaudeEnvOverride(entries)
}

func readClaudeEnvOverride(path string) ([]byte, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func decodeClaudeEnvEntries(data []byte) (map[string]any, error) {
	var topLevel any
	if err := json.Unmarshal(data, &topLevel); err != nil {
		return nil, fmt.Errorf("override JSON: %w", err)
	}
	if topLevel == nil {
		return nil, fmt.Errorf("override JSON: top-level nullは許可されません")
	}
	raw, ok := topLevel.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("override JSON: top-levelはobjectのみ許可されます")
	}
	for key := range raw {
		if key != "env" {
			return nil, fmt.Errorf("top-level key %qは許可されません (envのみ)", key)
		}
	}

	envValue, hasEnv := raw["env"]
	if !hasEnv {
		return nil, nil
	}
	if envValue == nil {
		return nil, fmt.Errorf("override env: nullは許可されません (objectまたは空object)")
	}
	entries, ok := envValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("override env: objectのみ許可されます")
	}
	return entries, nil
}

func buildClaudeEnvOverride(entries map[string]any) (claudeEnvOverride, error) {
	if entries == nil {
		return claudeEnvOverride{}, nil
	}
	override := claudeEnvOverride{sets: make(map[string]string, len(entries))}
	for key, value := range entries {
		switch typed := value.(type) {
		case string:
			override.sets[key] = typed
		case nil:
			override.deletes = append(override.deletes, key)
		default:
			return claudeEnvOverride{}, fmt.Errorf("override env %qはstringかnullのみ許可されます", key)
		}
	}
	return override, nil
}
