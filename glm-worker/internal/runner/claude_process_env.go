package runner

const configuredClaudeSubprocessEnvScrub = "1"

func claudeProcessEnvAdditions() map[string]string {
	additions := claudeInvocationEnvDefaults()
	additions["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] = configuredClaudeSubprocessEnvScrub
	return additions
}
