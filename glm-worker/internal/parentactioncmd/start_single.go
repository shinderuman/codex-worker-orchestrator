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

func executeStartCompatAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 || (len(args) == 3 && args[1] == "--rotation-claim") {
		return executeDirectWorkerAction(cfg, actionStart, args, stdout, stderr)
	}
	return executeStartSingleAction(cfg, args, stdout, stderr)
}

func executeStartSingleAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	extraEnv, err := explicitSingleStartEnv(args, startIdentityEnv(actionStart))
	if err != nil {
		return err
	}
	return withParentWaitLease(cfg, func() error {
		return runWorker(cfg.RepoRoot, directWorkerArgs(actionStart), nil, stdout, stderr, extraEnv)
	})
}

func explicitSingleStartEnv(args, baseEnv []string) ([]string, error) {
	if len(args) == 3 && args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle {
		return append(baseEnv, executionunit.DispositionEnv+"="+executionunit.ExecutionUnitSingle), nil
	}
	if len(args) == 5 &&
		args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle &&
		args[3] == "--rotation-claim" && state.ValidGeneratedUUID(args[4]) {
		return append(baseEnv,
			executionunit.DispositionEnv+"="+executionunit.ExecutionUnitSingle,
			state.SessionRotationClaimIDEnv+"="+args[4],
		), nil
	}
	return nil, fmt.Errorf("%s", explicitSingleStartUsage)
}
