package parentactioncmd

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"

func runtimeInstallPath(path string) bool {
	return repositoryharness.RuntimeInstallPath(path)
}
