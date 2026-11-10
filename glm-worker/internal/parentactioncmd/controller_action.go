package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeControllerAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if !validControllerActionInvocation(args) {
		return fmt.Errorf("usage: glm-parent-action <controller-semantic|controller-execution|controller-publication|controller-evidence> <token>")
	}
	options := args[2:]
	payload, err := parentaction.Peek(cfg.RepoRoot, args[0], args[1])
	if err != nil {
		return err
	}
	command, err := decodeControllerStaging(cfg, args[0], args[1], payload, options)
	if err != nil {
		return err
	}
	var operation struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(command, &operation); err != nil {
		return err
	}
	recorder := state.NewStageRecorder(state.AttachStateStore(cfg))
	recorder.SetClockOrigin("parent-command")
	recorder.BindCurrentTask()
	defer recorder.FlushOrArchive()
	run := func() error {
		consume := func() error {
			_, err := parentaction.ConsumeExpected(cfg.RepoRoot, args[0], args[1], payload)
			return err
		}
		if args[0] == controllerPublicationAction {
			if handled, err := runTypedCompletionAction(cfg, command, consume, stdout, stderr); handled {
				return err
			}
		}
		worker, err := resolveControllerActionWorker(cfg, args[0], operation.Action, command)
		if err != nil {
			return err
		}
		if err := runResolvedWorker(worker, cfg.RepoRoot, []string{"--authority", args[0]}, bytes.NewReader(command), stdout, stderr, nil); err != nil {
			return err
		}
		_, err = parentaction.ConsumeExpected(cfg.RepoRoot, args[0], args[1], payload)
		return err
	}
	return recorder.Measure(args[0]+"-"+operation.Action, func() error {
		if args[0] == "controller-evidence" {
			return run()
		}
		return withParentWaitLease(cfg, run)
	})
}

func resolveControllerActionWorker(cfg config.AppConfig, authority, action string, command []byte) (string, error) {
	if authority == "controller-evidence" && action == exportBundleAction {
		return resolveInstalledExportWorker()
	}
	if authority != controllerPublicationAction || action != controller.PublicationOperationPostInstallSmoke {
		return resolveGLMWorker()
	}
	var published completionPublicationCommand
	if err := json.Unmarshal(command, &published); err != nil {
		return "", err
	}
	return installedPostInstallWorker(cfg, published)
}

func validControllerActionInvocation(args []string) bool {
	return len(args) >= 2 && parentaction.IsControllerAction(args[0]) &&
		(len(args) == 2 || (args[0] == controllerExecutionAction && parentactiongrammar.ValidExecutionOperationSelection(args[2:])))
}

func validateControllerCommandObject(payload []byte) error {
	var command map[string]json.RawMessage
	if err := json.Unmarshal(payload, &command); err != nil {
		return fmt.Errorf("controller command JSON: %w", err)
	}
	if command == nil {
		return fmt.Errorf("controller command must be a JSON object")
	}
	return nil
}

func decodeControllerStaging(cfg config.AppConfig, action, token string, payload []byte, options []string) ([]byte, error) {
	if action == controllerPublicationAction {
		command, err := mergePublicationStagedPayload(payload)
		if err != nil {
			return nil, err
		}
		return command, validatePreparedCompletionBinding(cfg, payload, command)
	}
	if action == controllerExecutionAction {
		if parentaction.TokenHasBoundTemplate(token) != (len(options) != 0) {
			return nil, fmt.Errorf("execution token and explicit operation selection must match")
		}
		if json.Valid(payload) {
			return nil, fmt.Errorf("controller-execution requires a machine-projected staging template")
		}
		command, err := mergeExecutionStagedPayload(payload)
		if err != nil {
			return nil, err
		}
		return command, validateExecutionStagedBinding(cfg, token, payload, options)
	}
	return payload, validateControllerCommandObject(payload)
}

func runTypedCompletionAction(cfg config.AppConfig, command []byte, consume func() error, stdout, stderr io.Writer) (bool, error) {
	var typed completionPublicationCommand
	if err := json.Unmarshal(command, &typed); err != nil {
		return true, err
	}
	switch typed.Action {
	case controller.PublicationOperationAccept:
		return true, runCompletionWorkflow(cfg, typed, consume, stdout, stderr)
	case controller.PublicationOperationValidationFailure, controller.PublicationOperationInstall:
		return true, resolveCompletionValidationFailure(cfg, typed, consume, stdout, stderr)
	default:
		return false, nil
	}
}
