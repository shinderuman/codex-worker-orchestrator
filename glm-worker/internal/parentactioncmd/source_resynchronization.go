package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type sourceResynchronizationStep struct {
	Surface    string `json:"surface"`
	Operation  string `json:"operation"`
	DurationMS int64  `json:"duration_ms"`
}

type sourceResynchronizationResult struct {
	Status               string                        `json:"status"`
	SourceAdopted        bool                          `json:"source_adopted"`
	Steps                []sourceResynchronizationStep `json:"steps"`
	NextSurface          string                        `json:"next_surface,omitempty"`
	NextOperation        string                        `json:"next_operation"`
	ProcessingDurationMS int64                         `json:"processing_duration_ms"`
}

func executeSourceResynchronization(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action resync-source")
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	return withParentWaitLease(cfg, func() error {
		return runSourceResynchronization(cfg, stdout, stderr)
	})
}

func runSourceResynchronization(cfg config.AppConfig, stdout, stderr io.Writer) error {
	started := time.Now()
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	origin, err := store.LoadHead()
	if err != nil {
		return err
	}
	if origin.RootTaskRef == nil || origin.Status != controller.ControllerStatusActive || origin.PendingTransitionID != "" {
		return fmt.Errorf("post-pull resynchronization requires active Task authority without pending transition")
	}
	steps := make([]sourceResynchronizationStep, 0, 4)
	sourceAdvanced := false
	const maxSteps = 12
	for range maxSteps {
		if err := requireSameResynchronizationTask(store, origin); err != nil {
			return err
		}
		operation, err := projectControllerCompletion(cfg)
		if err != nil {
			return fmt.Errorf("post-pull Controller projection: %w", err)
		}
		if !sourceResynchronizationAllowed(operation, sourceAdvanced) {
			return finishSourceResynchronization(cfg, stdout, started, operation, steps, sourceAdvanced)
		}
		stepStarted := time.Now()
		if err := performProjectedSourceOperation(cfg, operation, stderr); err != nil {
			return err
		}
		steps = append(steps, sourceResynchronizationStep{
			Surface: operation.Surface, Operation: operation.Operation,
			DurationMS: time.Since(stepStarted).Milliseconds(),
		})
		sourceAdvanced = sourceAdvanced || sourceResynchronizationAdvancesSource(operation.Operation)
	}
	return fmt.Errorf("post-pull Controller did not converge in %d typed operations", maxSteps)
}

func finishSourceResynchronization(cfg config.AppConfig, stdout io.Writer, started time.Time, operation controller.ParentCompletionOperation, steps []sourceResynchronizationStep, advanced bool) error {
	if operation.Operation == controller.PublicationOperationRecover {
		return fmt.Errorf("post-pull source resynchronization requires the projected Controller recovery before retry")
	}
	if err := verifyStoppedSourceResynchronization(cfg, len(steps) == 0); err != nil {
		return err
	}
	status := "no-mechanical-step"
	if advanced {
		status = "source-resynchronized"
	} else if len(steps) > 0 {
		status = "mechanical-recovery-applied"
	}
	return json.NewEncoder(stdout).Encode(sourceResynchronizationResult{
		Status: status, SourceAdopted: advanced, Steps: steps,
		NextSurface: operation.Surface, NextOperation: operation.Operation,
		ProcessingDurationMS: time.Since(started).Milliseconds(),
	})
}

func performProjectedSourceOperation(cfg config.AppConfig, operation controller.ParentCompletionOperation, stderr io.Writer) error {
	command, err := projectedSourceCommand(cfg, operation)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return err
	}
	var result bytes.Buffer
	if err := runWorker(cfg.RepoRoot, []string{"--authority", operation.Surface}, bytes.NewReader(encoded), &result, stderr, nil); err != nil {
		return fmt.Errorf("post-pull %s/%s failed; use the projected Controller recovery before retry: %w", operation.Surface, operation.Operation, err)
	}
	_, err = decodeSingleMachineJSON(result.Bytes(), "post-pull Controller transition")
	return err
}

func requireSameResynchronizationTask(store *controller.Store, origin controller.RepositoryControllerHead) error {
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(origin.RootTaskRef, head.RootTaskRef) || !reflect.DeepEqual(origin.ExecutionTaskRef, head.ExecutionTaskRef) {
		return fmt.Errorf("post-pull resynchronization refuses a different Task authority")
	}
	return nil
}

func projectedSourceCommand(cfg config.AppConfig, operation controller.ParentCompletionOperation) (any, error) {
	switch operation.Surface {
	case controller.PublicationOperationSurface:
		return projectedSourcePublicationCommand(cfg, operation)
	case controller.ExecutionOperationSurface:
		if operation.Execution == nil || operation.Execution.Action != operation.Operation {
			return nil, fmt.Errorf("post-pull execution projection has no matching typed command")
		}
		return operation.Execution, nil
	default:
		return nil, fmt.Errorf("post-pull transition has no Controller owner")
	}
}

func projectedSourcePublicationCommand(cfg config.AppConfig, operation controller.ParentCompletionOperation) (any, error) {
	if operation.Command == nil || operation.Command.Action != operation.Operation {
		return nil, fmt.Errorf("post-pull publication projection has no matching typed command")
	}
	command := *operation.Command
	switch command.Action {
	case controller.PublicationOperationAdopt, controller.PublicationOperationAdoptSource:
		command.Action = controller.PublicationOperationResyncRetired
	case controller.PublicationOperationAdoptMutable:
		command.Action = controller.PublicationOperationResyncMutable
		sourceOID, err := projectedPulledPrimaryOID(cfg)
		if err != nil {
			return nil, err
		}
		command.SourceOID = sourceOID
	case controller.PublicationOperationRebind:
		command.Action = controller.PublicationOperationResyncRebind
	default:
		return nil, fmt.Errorf("post-pull operation %s has no non-pushing Controller owner", command.Action)
	}
	if command.SourceOID == "" {
		return nil, fmt.Errorf("post-pull source OID is missing")
	}
	return &command, nil
}

func projectedPulledPrimaryOID(cfg config.AppConfig) (string, error) {
	store, err := controller.Open(cfg)
	if err != nil {
		return "", err
	}
	primary, err := controller.CaptureWorkspaceSnapshot(store.Identity().PrimaryRoot)
	if err != nil {
		return "", err
	}
	return primary.Head, nil
}

func sourceResynchronizationAdvancesSource(action string) bool {
	switch action {
	case controller.PublicationOperationAdoptMutable, controller.PublicationOperationRebind,
		controller.PublicationOperationAdopt, controller.PublicationOperationAdoptSource:
		return true
	default:
		return false
	}
}

func sourceResynchronizationAllowed(operation controller.ParentCompletionOperation, started bool) bool {
	switch operation.Surface {
	case controller.PublicationOperationSurface:
		if sourceResynchronizationAdvancesSource(operation.Operation) {
			return true
		}
		return false
	case controller.ExecutionOperationSurface:
		switch operation.Operation {
		case "cleanup", "materialize":
			return started
		}
	}
	return false
}

func verifyStoppedSourceResynchronization(cfg config.AppConfig, firstStep bool) error {
	store, err := controller.Open(cfg)
	if err != nil {
		return err
	}
	head, err := store.LoadHead()
	if err != nil {
		return err
	}
	if head.Status != controller.ControllerStatusActive {
		return fmt.Errorf("post-pull source resynchronization stopped: Controller is fail-closed")
	}
	if !firstStep || head.IntegrationTip == "" {
		return nil
	}
	primary, err := controller.CaptureWorkspaceSnapshot(store.Identity().PrimaryRoot)
	if err != nil {
		return err
	}
	if primary.Head != head.IntegrationTip {
		return fmt.Errorf("post-pull primary differs from Controller integration source; resynchronization requires an admitted typed Controller transition")
	}
	return nil
}
