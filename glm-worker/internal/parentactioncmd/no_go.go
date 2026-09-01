package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type noGoOutput struct {
	Status    string `json:"status"`
	Completed bool   `json:"completed"`
}

func executeNoGo(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action no-go")
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	return runNoGo(cfg, stdout)
}

func runNoGo(cfg config.AppConfig, stdout io.Writer) error {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := app.AcquireRepoLock(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	if !plan.Allows(state.ParentActionNoGo) {
		return fmt.Errorf("terminal no-go is not allowed for the current task")
	}
	completed, err := st.CompleteObservationNoGo()
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(noGoOutput{Status: "no-go", Completed: completed})
}
