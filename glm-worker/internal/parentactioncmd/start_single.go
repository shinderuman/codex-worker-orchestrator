package parentactioncmd

import (
	"fmt"
	"io"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const explicitSingleStartUsage = "usage: glm-parent-action start --execution-unit single [--rotation-claim <claim-id>]"

func executeStartSingleAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	extraEnv, err := explicitSingleStartEnv(args, startIdentityEnv(actionStart))
	if err != nil {
		return err
	}

	runErr := withParentWaitLease(cfg, func() error {
		return runWorker(cfg.RepoRoot, directWorkerArgs(actionStart), nil, stdout, stderr, extraEnv)
	})
	recordErr := recordSingleExecutionUnitDisposition(cfg)
	if runErr != nil {
		return runErr
	}
	return recordErr
}

func explicitSingleStartEnv(args, baseEnv []string) ([]string, error) {
	if len(args) == 3 && args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle {
		return baseEnv, nil
	}
	if len(args) == 5 &&
		args[1] == parentactiongrammar.ExecutionUnitOption && args[2] == executionunit.ExecutionUnitSingle &&
		args[3] == "--rotation-claim" && state.ValidGeneratedUUID(args[4]) {
		return append(baseEnv, state.SessionRotationClaimIDEnv+"="+args[4]), nil
	}
	return nil, fmt.Errorf("%s", explicitSingleStartUsage)
}

func recordSingleExecutionUnitDisposition(cfg config.AppConfig) error {
	st := state.AttachStateStore(cfg)
	return executionunit.RecordDisposition(
		st,
		st.ReadOr("active-task", ""),
		executionunit.ExecutionUnitSingle,
		time.Now().UTC(),
	)
}
