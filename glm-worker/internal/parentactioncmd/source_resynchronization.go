package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

// sourceResynchronizationStep records only operations performed by the existing
// Controller owners. It does not grant Git or Review authority.
type sourceResynchronizationStep struct {
	Surface   string `json:"surface"`
	Operation string `json:"operation"`
}

// executeSourceResynchronization is invoked after an externally authorized
// fast-forward pull. It never runs git pull or a Reviewer model.
func executeSourceResynchronization(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action resync-source")
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	return withParentWaitLease(cfg, func() error {
		steps := make([]sourceResynchronizationStep, 0, 4)
		sourceAdvanced := false
		const maxSteps = 12
		for range maxSteps {
			operation, err := projectControllerCompletion(cfg)
			if err != nil {
				return fmt.Errorf("post-pull Controller projection: %w", err)
			}
			if !sourceResynchronizationAllowed(operation, len(steps) != 0) {
				status := "no-mechanical-step"
				if sourceAdvanced {
					status = "source-resynchronized"
				} else if len(steps) > 0 {
					status = "mechanical-recovery-applied"
				}
				return json.NewEncoder(stdout).Encode(struct {
					Status        string                        `json:"status"`
					SourceAdopted bool                          `json:"source_adopted"`
					Steps         []sourceResynchronizationStep `json:"steps"`
					NextSurface   string                        `json:"next_surface,omitempty"`
					NextOperation string                        `json:"next_operation"`
				}{
					Status: status, SourceAdopted: sourceAdvanced, Steps: steps,
					NextSurface: operation.Surface, NextOperation: operation.Operation,
				})
			}
			var command any
			switch operation.Surface {
			case controller.PublicationOperationSurface:
				if operation.Command == nil || operation.Command.Action != operation.Operation {
					return fmt.Errorf("post-pull publication projection has no matching typed command")
				}
				command = operation.Command
			case controller.ExecutionOperationSurface:
				if operation.Execution == nil || operation.Execution.Action != operation.Operation {
					return fmt.Errorf("post-pull execution projection has no matching typed command")
				}
				command = operation.Execution
			default:
				return fmt.Errorf("post-pull transition has no Controller owner")
			}
			encoded, err := json.Marshal(command)
			if err != nil {
				return err
			}
			var result bytes.Buffer
			if err := runWorker(cfg.RepoRoot, []string{"--authority", operation.Surface}, bytes.NewReader(encoded), &result, stderr, nil); err != nil {
				return fmt.Errorf("post-pull %s/%s failed; use the projected Controller recovery before retry: %w", operation.Surface, operation.Operation, err)
			}
			if _, err := decodeSingleMachineJSON(result.Bytes(), "post-pull Controller transition"); err != nil {
				return err
			}
			steps = append(steps, sourceResynchronizationStep{Surface: operation.Surface, Operation: operation.Operation})
			if sourceResynchronizationAdvancesSource(operation.Operation) {
				sourceAdvanced = true
			}
		}
		return fmt.Errorf("post-pull Controller did not converge in %d typed operations", maxSteps)
	})
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
