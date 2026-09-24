package runner

import (
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type SensitiveArtifactValue struct {
	Category string `json:"category"`
	Value    string `json:"-"`
}

const (
	SensitiveArtifactProviderAuthToken = "provider-auth-token"
	SensitiveArtifactProviderAPIKey    = "provider-api-key"
)

func SensitiveArtifactValues(cfg config.AppConfig) ([]SensitiveArtifactValue, error) {
	settingEnv, deletes, err := loadConfiguredSettingEnv(cfg)
	if err != nil {
		return nil, err
	}
	childEnv := buildChildEnv(cfg.EnvAllowlist, settingEnv, nil, deletes)
	values := make([]SensitiveArtifactValue, 0, 2)
	for _, item := range childEnv {
		key, value, ok := strings.Cut(item, "=")
		if !ok || value == "" {
			continue
		}
		switch key {
		case "ANTHROPIC_AUTH_TOKEN":
			values = append(values, SensitiveArtifactValue{Category: SensitiveArtifactProviderAuthToken, Value: value})
		case "ANTHROPIC_API_KEY":
			values = append(values, SensitiveArtifactValue{Category: SensitiveArtifactProviderAPIKey, Value: value})
		}
	}
	return values, nil
}
