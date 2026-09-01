package runner

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"

type claudeEnvOverride struct {
	sets    map[string]string
	deletes []string
}

func parseClaudeEnvOverride(path string) (claudeEnvOverride, error) {
	override, err := claudeoverride.Load(path)
	if err != nil {
		return claudeEnvOverride{}, err
	}
	return claudeEnvOverride{sets: override.Sets, deletes: override.Deletes}, nil
}
