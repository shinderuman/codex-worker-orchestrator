package parentactioncmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const actionReviewEvidence = "review-evidence"

func executeParentReviewEvidence(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != actionReviewEvidence {
		return fmt.Errorf("usage: glm-parent-action review-evidence")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	return parentevidence.PrintReviewEvidence(cfg.RepoRoot, st, stdout)
}
