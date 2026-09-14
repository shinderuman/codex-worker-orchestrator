package runner

import "path/filepath"

func loadSettingEnv(claudeConfigDir string, overridePath string) (map[string]string, []string, error) {
	configDir, err := resolveClaudeConfigDir(claudeConfigDir)
	if err != nil {
		return nil, nil, err
	}
	return loadSettingEnvPath(filepath.Join(configDir, "settings.json"), overridePath)
}
