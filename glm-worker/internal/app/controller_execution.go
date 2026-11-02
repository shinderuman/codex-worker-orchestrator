package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const (
	controllerExecutionSurface   = "controller-execution"
	controllerPublicationSurface = "controller-publication"
)

func runControllerOperations(args []string, loadConfig func() (config.AppConfig, error), stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag {
		return false, nil
	}
	var command any
	switch args[1] {
	case controllerExecutionSurface:
		command = &workflow.ControllerExecutionCommand{}
	case controllerPublicationSurface:
		command = &controllerPublicationCommand{}
	default:
		return false, nil
	}
	cfg, store, err := decodeControllerCommand(loadConfig, stdin, command)
	if err != nil {
		return true, err
	}
	lock, err := acquireWorkflowLock(cfg)
	if err != nil {
		return true, err
	}
	defer func() { _ = lock.Close() }()
	result, err := dispatchControllerOperation(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, result)
}

func dispatchControllerOperation(cfg config.AppConfig, store *controller.Store, command any) (any, error) {
	switch value := command.(type) {
	case *workflow.ControllerExecutionCommand:
		return workflow.ExecuteControllerExecution(cfg, store, *value)
	case *controllerPublicationCommand:
		return executeControllerPublication(cfg, store, *value)
	default:
		return nil, fmt.Errorf("unsupported controller command type")
	}
}

func decodeControllerCommand(loadConfig func() (config.AppConfig, error), stdin io.Reader, command any) (config.AppConfig, *controller.Store, error) {
	cfg, err := loadConfig()
	if err != nil {
		return cfg, nil, err
	}
	store, err := openControllerSemanticStore(cfg)
	if err != nil {
		return cfg, nil, err
	}
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(command); err != nil {
		return cfg, nil, err
	}
	if err := requireControllerEvidenceEOF(decoder); err != nil {
		return cfg, nil, err
	}
	return cfg, store, nil
}
