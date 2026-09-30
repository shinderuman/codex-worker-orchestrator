package parentactioncmd

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func executeRepositoryAwareResume(cfg config.AppConfig, stdout, stderr io.Writer, extraEnv []string) error {
	return runWorker(cfg.RepoRoot, directWorkerArgs(actionResume), nil, stdout, stderr, extraEnv)
}
