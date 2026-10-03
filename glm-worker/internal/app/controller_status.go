package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func runControllerStatus(
	args []string,
	loadConfig func() (config.AppConfig, error),
	stdout io.Writer,
) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-status" {
		return false, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	store, err := openControllerSemanticStore(cfg)
	if err != nil {
		return true, err
	}
	report, err := store.ProjectStatus()
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, report)
}
