package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func loadConfiguredSettingEnv(cfg config.AppConfig) (map[string]string, []string, error) {
	settingsPath := cfg.ClaudeSettingsPath
	if settingsPath == "" {
		configDir, err := resolveClaudeConfigDir(cfg.ClaudeConfigDir)
		if err != nil {
			return nil, nil, err
		}
		settingsPath = filepath.Join(configDir, "settings.json")
	}
	return loadSettingEnvPath(settingsPath, cfg.ClaudeSettingsOverride)
}

func loadSettingEnvPath(settingsPath, overridePath string) (map[string]string, []string, error) {
	result := make(map[string]string)
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		var parsed struct {
			Env map[string]string `json:"env"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, nil, fmt.Errorf("claude settingsを解析できません: %w", err)
		}
		for _, key := range essentialSettingEnvKeys {
			if value, ok := parsed.Env[key]; ok && value != "" {
				result[key] = value
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("claude settingsを読み込めません: %w", err)
	}

	override, err := parseClaudeEnvOverride(overridePath)
	if err != nil {
		return nil, nil, fmt.Errorf("env override: %w", err)
	}
	for _, key := range override.deletes {
		delete(result, key)
	}
	for key, value := range override.sets {
		result[key] = value
	}
	return result, override.deletes, nil
}
