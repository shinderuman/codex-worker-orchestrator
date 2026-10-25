package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func runControllerExecutionProjection(args []string, loadConfig func() (config.AppConfig, error), stdout io.Writer) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-execution-plan" {
		return false, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	projection := struct {
		Consistent     bool              `json:"consistent"`
		RequiredAction *string           `json:"required_action"`
		Parameters     map[string]string `json:"required_action_parameters,omitempty"`
	}{Consistent: handoff.Consistent, RequiredAction: handoff.RequiredAction}
	if handoff.RequiredAction != nil && *handoff.RequiredAction == controllerExecutionSurface {
		projection.Parameters = handoff.RequiredActionParameters
	}
	return true, writeValidatedMachineJSON(stdout, projection)
}
