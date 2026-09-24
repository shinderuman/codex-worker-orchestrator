package runner

import "strconv"

const configuredAutoCompactWindowTokens = 500_000
const configuredAlwaysEnableEffort = "1"
const configuredClaudeSubprocessEnvScrub = "1"

var configuredAutoCompactWindowArgument = formatAutoCompactWindowArgument(configuredAutoCompactWindowTokens)

func formatAutoCompactWindowArgument(tokens int) string {
	if tokens%1_000 == 0 {
		return strconv.Itoa(tokens/1_000) + "k"
	}
	return strconv.Itoa(tokens)
}

func claudeManagedEnvDefaults() map[string]string {
	return map[string]string{
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":  strconv.Itoa(configuredAutoCompactWindowTokens),
		"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT": configuredAlwaysEnableEffort,
	}
}

func claudeInvocationEnvDefaults() map[string]string {
	defaults := claudeManagedEnvDefaults()
	defaults["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] = configuredClaudeSubprocessEnvScrub
	return defaults
}
