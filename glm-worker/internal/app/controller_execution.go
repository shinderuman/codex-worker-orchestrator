package app

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type controllerExecutionCommand struct {
	Action             string                        `json:"action"`
	ExpectedGeneration uint64                        `json:"expected_generation"`
	EpisodeID          string                        `json:"episode_id,omitempty"`
	EpisodeRevision    uint64                        `json:"episode_revision,omitempty"`
	SuspensionID       string                        `json:"suspension_id,omitempty"`
	WorkspaceID        string                        `json:"workspace_id,omitempty"`
	SealRef            *controller.EvidenceObjectRef `json:"seal_ref,omitempty"`
	TransitionID       string                        `json:"transition_id,omitempty"`
}

func runControllerOperations(args []string, loadConfig func() (config.AppConfig, error), stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag {
		return false, nil
	}
	var command any
	switch args[1] {
	case "controller-execution":
		command = &controllerExecutionCommand{}
	case "controller-publication":
		command = &controllerPublicationCommand{}
	default:
		return false, nil
	}
	cfg, store, err := decodeControllerCommand(loadConfig, stdin, command)
	if err != nil {
		return true, err
	}
	result, err := dispatchControllerOperation(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, result)
}

func dispatchControllerOperation(cfg config.AppConfig, store *controller.Store, command any) (any, error) {
	switch value := command.(type) {
	case *controllerExecutionCommand:
		return executeControllerExecution(cfg, store, *value)
	case *controllerPublicationCommand:
		return executeControllerPublication(cfg, store, *value)
	default:
		return nil, fmt.Errorf("unsupported controller command type")
	}
}

func executeControllerExecution(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.Action == "recover" {
		if command.TransitionID == "" {
			return controller.ExecutionOperationResult{}, fmt.Errorf("execution recovery requires transition identity")
		}
		return store.RecoverExecutionOperation(command.TransitionID)
	}
	head, err := store.LoadHead()
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	if command.ExpectedGeneration != head.ControllerGeneration {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution command generation is stale")
	}
	switch command.Action {
	case "suspend":
		return executeControllerSuspend(cfg, store, command)
	case "materialize":
		return store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: command.ExpectedGeneration, EpisodeID: command.EpisodeID, EpisodeRevision: command.EpisodeRevision, SuspensionID: command.SuspensionID})
	case "cleanup":
		if command.SealRef == nil || command.WorkspaceID == "" {
			return controller.ExecutionOperationResult{}, fmt.Errorf("execution cleanup requires sealed workspace identity")
		}
		return store.CleanupExecution(controller.CleanupExecutionInput{ExpectedGeneration: command.ExpectedGeneration, WorkspaceID: command.WorkspaceID, SealRef: *command.SealRef})
	case "gc-suspension":
		return store.GarbageCollectSuspension(command.ExpectedGeneration, command.SuspensionID)
	default:
		return controller.ExecutionOperationResult{}, fmt.Errorf("unsupported controller execution action %q", command.Action)
	}
}

func executeControllerSuspend(cfg config.AppConfig, store *controller.Store, command controllerExecutionCommand) (controller.ExecutionOperationResult, error) {
	if command.EpisodeID == "" || command.EpisodeRevision == 0 {
		return controller.ExecutionOperationResult{}, fmt.Errorf("execution suspension requires episode authority")
	}
	admission, err := currentControllerSemanticAdmission(cfg, store)
	if err != nil {
		return controller.ExecutionOperationResult{}, err
	}
	return store.SuspendExecution(admission, command.EpisodeID, command.EpisodeRevision)
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
