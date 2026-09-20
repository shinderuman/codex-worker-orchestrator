package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentfix"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type reopenOutput struct {
	Status         string   `json:"status"`
	TaskStatus     string   `json:"task_status"`
	RequiredAction string   `json:"required_action"`
	AllowedActions []string `json:"allowed_actions"`
}

const reopenUsage = "usage: glm-parent-action reopen [--origin <origin>] [--cause <cause>]"

func executeParentLifecycleAction(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if args[0] == actionReopen {
		return executeReopen(cfg, args, stdout)
	}
	return executeNoGo(cfg, args, stdout)
}

func executeReopen(cfg config.AppConfig, args []string, stdout io.Writer) error {
	origin, cause, err := reopenOptions(args[1:])
	if err != nil {
		return err
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	plan, admitted, err := st.AdmitParentAction(state.ParentActionReopen)
	if err != nil {
		return err
	}
	if !admitted || !plan.Allows(state.ParentActionReopen) {
		return fmt.Errorf("reopen is not admitted for the current task (required action %s)", plan.RequiredAction)
	}
	if err := st.ReopenAcceptedParentCompletion(origin, cause); err != nil {
		return err
	}
	after, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	output := reopenOutput{
		Status:         "reopened",
		TaskStatus:     string(st.TaskStatus()),
		RequiredAction: string(after.RequiredAction),
		AllowedActions: []string{},
	}
	for _, action := range after.AllowedActions {
		output.AllowedActions = append(output.AllowedActions, string(action))
	}
	return json.NewEncoder(stdout).Encode(output)
}

func reopenOptions(args []string) (string, string, error) {
	options, remaining, err := parentfix.Extract(args)
	if err != nil || len(remaining) != 0 || options.AcceptedScope != "" {
		return "", "", fmt.Errorf("%s", reopenUsage)
	}
	return options.Origin, options.Cause, nil
}
