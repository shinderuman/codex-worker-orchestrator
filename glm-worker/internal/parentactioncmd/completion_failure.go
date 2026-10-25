package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

const completionRetry = "retry"
const completionReenter = "reenter"

func resolveCompletionValidationFailure(cfg config.AppConfig, command completionPublicationCommand, consume func() error, stdout, stderr io.Writer) error {
	result := completionWorkflowResult{Status: "blocked", SourceOID: command.SourceOID}
	if command.CandidateID == "" || command.SourceOID == "" {
		return fmt.Errorf("completion failure lacks its candidate source")
	}
	switch command.FailureDisposition {
	case completionRetry:
		if err := consume(); err != nil {
			return err
		}
		ready, err := retryCompletionBoundary(cfg, command, &result, stderr)
		if err != nil {
			return writeCompletionWorkflowFailure(stdout, result, err)
		}
		if !ready {
			return json.NewEncoder(stdout).Encode(result)
		}
		return driveCompletionWorkflow(cfg, result, stdout, stderr)
	case completionReenter:
		return reenterCompletionSource(cfg, command, result, consume, stdout, stderr)
	default:
		return fmt.Errorf("completion failure requires a semantic retry or reenter disposition")
	}
}

func retryCompletionBoundary(cfg config.AppConfig, command completionPublicationCommand, result *completionWorkflowResult, stderr io.Writer) (bool, error) {
	if command.Action != controller.PublicationOperationInstall {
		return runCompletionFinalValidation(cfg, command.FinalValidation, result, "retry-final-validation:")
	}
	command.FailureDisposition = ""
	if _, err := runCompletionPublication(cfg, command, nil, stderr); err != nil {
		return false, err
	}
	result.Steps = append(result.Steps, command.Action)
	return true, nil
}

func reenterCompletionSource(cfg config.AppConfig, command completionPublicationCommand, result completionWorkflowResult, consume func() error, stdout, stderr io.Writer) error {
	command.Action, command.FailureDisposition, command.FinalValidation = completionReenter, "", nil
	command.SourceOID = ""
	raw, err := runCompletionPublication(cfg, command, nil, stderr)
	if err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	if err := consume(); err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	_, err = stdout.Write(append(raw, '\n'))
	return err
}
