package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	steps := make([]sourceResynchronizationStep, 0, 4)
	sourceAdvanced := false
	const maxSteps = 12
	for range maxSteps {
		operation, err := projectControllerCompletion(cfg)
		if err != nil {
			return fmt.Errorf("post-pull Controller projection: %w", err)
		}
		if !sourceResynchronizationAllowed(operation, len(steps) != 0) {
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
	if len(steps) == 0 && operation.Operation == controller.PublicationOperationRecover {
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
	command, err := projectedSourceCommand(operation)
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

func projectedSourceCommand(operation controller.ParentCompletionOperation) (any, error) {
	switch operation.Surface {
	case controller.PublicationOperationSurface:
		if operation.Command == nil || operation.Command.Action != operation.Operation {
			return nil, fmt.Errorf("post-pull publication projection has no matching typed command")
		}
		return operation.Command, nil
	case controller.ExecutionOperationSurface:
		if operation.Execution == nil || operation.Execution.Action != operation.Operation {
			return nil, fmt.Errorf("post-pull execution projection has no matching typed command")
		}
		return operation.Execution, nil
	default:
		return nil, fmt.Errorf("post-pull transition has no Controller owner")
	}
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
		return started && operation.Operation == controller.PublicationOperationRecover
	case controller.ExecutionOperationSurface:
		switch operation.Operation {
		case "cleanup", "materialize":
			return true
		case controller.PublicationOperationRecover:
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
