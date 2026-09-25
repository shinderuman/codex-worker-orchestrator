package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

const anthropicBaseURLEnv = "ANTHROPIC_BASE_URL"

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

func loadInvocationSettingEnv(cfg config.AppConfig) (map[string]string, []string, error) {
	settingEnv, deletes, err := loadConfiguredSettingEnv(cfg)
	if err != nil {
		return nil, nil, err
	}
	if cfg.ClaudeSettingsPath != "" {
		if err := validateManagedProviderRoute(cfg, settingEnv, deletes); err != nil {
			return nil, nil, err
		}
	}
	return settingEnv, deletes, nil
}

func validateManagedProviderRoute(cfg config.AppConfig, settingEnv map[string]string, deletes []string) error {
	baseURL := strings.TrimSpace(settingEnv[anthropicBaseURLEnv])
	if baseURL == "" && !stringListContains(deletes, anthropicBaseURLEnv) && stringListContains(cfg.EnvAllowlist, anthropicBaseURLEnv) {
		baseURL = strings.TrimSpace(os.Getenv(anthropicBaseURLEnv))
	}
	if baseURL == "" {
		return fmt.Errorf("managed Claude runtimeの%sがありません: Anthropic既定providerへのfallbackを拒否します", anthropicBaseURLEnv)
	}
	if routesToAnthropicProvider(baseURL) {
		return fmt.Errorf("managed Claude runtimeの%sがunsupported Anthropic providerを指しています", anthropicBaseURLEnv)
	}
	return nil
}

func routesToAnthropicProvider(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	return host == "anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}

func stringListContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
