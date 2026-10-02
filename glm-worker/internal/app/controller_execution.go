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

func runControllerExecution(args []string, loadConfig func() (config.AppConfig, error), stdin io.Reader, stdout io.Writer) (bool, error) {
	if len(args) != 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-execution" {
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
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	var command controllerExecutionCommand
	if err := decoder.Decode(&command); err != nil {
		return true, err
	}
	if err := requireControllerEvidenceEOF(decoder); err != nil {
		return true, err
	}
	result, err := executeControllerExecution(cfg, store, command)
	if err != nil {
		return true, err
	}
	return true, writeValidatedMachineJSON(stdout, result)
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
