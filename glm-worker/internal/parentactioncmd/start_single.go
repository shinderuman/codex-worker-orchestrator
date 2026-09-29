package parentactioncmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const explicitSingleStartUsage = "usage: glm-parent-action start --execution-unit single [--rotation-claim <claim-id>]"

func executeStartSingleAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	extraEnv := startIdentityEnv(actionStart)
	if len(args) == 3 && args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle {
		// Explicit single-unit disposition is required at the new-task boundary.
	} else if len(args) == 5 &&
		args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle &&
		args[3] == "--rotation-claim" && state.ValidGeneratedUUID(args[4]) {
		extraEnv = append(extraEnv, state.SessionRotationClaimIDEnv+"="+args[4])
	} else {
		return fmt.Errorf("%s", explicitSingleStartUsage)
	}
	return withParentWaitLease(cfg, func() error {
		return runWorker(cfg.RepoRoot, directWorkerArgs(actionStart), nil, stdout, stderr, extraEnv)
	})
}
