package runner

import "strings"

type contextWindowConfig struct {
	resolvedModelID               string
	knownModelContextWindowTokens int
	declaredMaxContextTokens      int
	source                        string
}

const configuredAutoCompactWindowTokens = 500_000
const configuredAutoCompactWindowArgument = "500k"
const defaultUnknownModelContextWindowTokens = 200_000
const zaiModelContextWindowSource = "zai-model-spec"

var zaiModelContextWindowTokens = map[string]int{
	"glm-4.7": 200_000,
	"glm-5.3": 1_000_000,
}

func contextWindowForModel(model string, settingEnv map[string]string) contextWindowConfig {
	resolved := resolveClaudeModelID(model, settingEnv)
	result := contextWindowConfig{resolvedModelID: resolved}
	window, ok := zaiModelContextWindowTokens[strings.ToLower(resolved)]
	if !ok {
		return result
	}
	result.knownModelContextWindowTokens = window
	result.source = zaiModelContextWindowSource
	if window > defaultUnknownModelContextWindowTokens {
		result.declaredMaxContextTokens = window
	}
	return result
}

func resolveClaudeModelID(model string, settingEnv map[string]string) string {
	key := ""
	switch strings.ToLower(model) {
	case "opus":
		key = "ANTHROPIC_DEFAULT_OPUS_MODEL"
	case "sonnet":
		key = "ANTHROPIC_DEFAULT_SONNET_MODEL"
	case "haiku":
		key = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	}
	if key != "" && settingEnv[key] != "" {
		return settingEnv[key]
	}
	return model
}
