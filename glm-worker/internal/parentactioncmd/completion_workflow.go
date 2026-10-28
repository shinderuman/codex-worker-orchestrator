package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type completionPublicationCommand struct {
	controller.PublicationParentCommand
	Message string `json:"message,omitempty"`
}

type completionWorkflowResult struct {
	Status       string   `json:"status"`
	Steps        []string `json:"steps"`
	SourceOID    string   `json:"source_oid,omitempty"`
	FinalOID     string   `json:"final_oid,omitempty"`
	RemoteOID    string   `json:"remote_oid,omitempty"`
	StopBoundary string   `json:"stop_boundary,omitempty"`
	Failure      any      `json:"failure,omitempty"`
}

func runCompletionWorkflow(cfg config.AppConfig, command completionPublicationCommand, consume func() error, stdout, stderr io.Writer) error {
	result := completionWorkflowResult{Status: "blocked"}
	if command.Policy == nil || command.Message == "" {
		return fmt.Errorf("completion requires its machine policy and semantic message")
	}
	var accepted controller.ExecutionOperationResult
	if _, err := runCompletionPublication(cfg, command, &accepted, stderr); err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	if accepted.CandidateRef == nil {
		return writeCompletionWorkflowFailure(stdout, result, fmt.Errorf("completion did not fix a candidate"))
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	result.SourceOID = candidate.CommitOID
	result.Steps = append(result.Steps, command.Action)
	if err := consume(); err != nil {
		return writeCompletionWorkflowFailure(stdout, result, err)
	}
	return driveCompletionWorkflow(cfg, result, stdout, stderr)
}

func runCompletionPublication(cfg config.AppConfig, command completionPublicationCommand, decoded any, stderr io.Writer) (json.RawMessage, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := runWorker(cfg.RepoRoot, []string{"--authority", controllerPublicationAction}, bytes.NewReader(body), &output, stderr, nil); err != nil {
		return nil, err
	}
	raw, err := decodeSingleMachineJSON(output.Bytes(), "completion publication")
	if err != nil {
		return nil, err
	}
	if decoded != nil {
		if err := json.Unmarshal(raw, decoded); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

func driveCompletionWorkflow(cfg config.AppConfig, result completionWorkflowResult, stdout, stderr io.Writer) error {
	for step := 0; step < 12; step++ {
		operation, err := projectControllerCompletion(cfg)
		if err != nil {
			return writeCompletionWorkflowFailure(stdout, result, err)
		}
		done, err := completionTerminalBoundary(operation)
		if err != nil {
			return writeCompletionWorkflowFailure(stdout, result, err)
		}
		if done {
			result.Status, result.StopBoundary = "completed", "next-task-not-started"
			return json.NewEncoder(stdout).Encode(result)
		}
		progressed, err := runCompletionStep(cfg, operation, &result, stderr)
		if err != nil {
			return writeCompletionWorkflowFailure(stdout, result, err)
		}
		if !progressed {
			return json.NewEncoder(stdout).Encode(result)
		}
	}
	return writeCompletionWorkflowFailure(stdout, result, fmt.Errorf("completion exceeded its bounded machine transitions"))
}

func completionTerminalBoundary(operation controller.ParentCompletionOperation) (bool, error) {
	switch operation.Operation {
	case controller.PublicationOperationSuccessorStart, controller.PublicationOperationObserved:
		return true, nil
	case controller.PublicationOperationNone:
		return false, fmt.Errorf("completion stopped before terminal publication: %s", operation.Reason)
	default:
		return false, nil
	}
}

func runCompletionStep(cfg config.AppConfig, operation controller.ParentCompletionOperation, result *completionWorkflowResult, stderr io.Writer) (bool, error) {
	if operation.Surface == controller.FinalValidationSurface {
		return runCompletionFinalValidation(cfg, operation.FinalValidation, result, "finalize-check:")
	}
	if operation.Command == nil {
		return false, fmt.Errorf("completion requires a parent disposition at %s", operation.Operation)
	}
	switch operation.Operation {
	case controller.PublicationOperationPromote, controller.PublicationOperationPublish, controller.PublicationOperationRecover, controller.PublicationOperationInstall, controller.PublicationOperationPostInstallSmoke:
	default:
		return false, fmt.Errorf("completion requires a parent disposition at %s", operation.Operation)
	}
	raw, err := runCompletionPublication(cfg, completionPublicationCommand{PublicationParentCommand: *operation.Command}, nil, stderr)
	if err != nil {
		return false, err
	}
	result.Steps = append(result.Steps, operation.Operation)
	var published struct {
		Head struct {
			IntegrationTip string `json:"integration_tip"`
			Remote         string `json:"observed_remote_tip"`
		} `json:"head"`
	}
	if err := json.Unmarshal(raw, &published); err != nil {
		return false, err
	}
	result.FinalOID, result.RemoteOID = published.Head.IntegrationTip, published.Head.Remote
	return true, nil
}

func runCompletionFinalValidation(cfg config.AppConfig, request *controller.FinalValidationRequest, result *completionWorkflowResult, label string) (bool, error) {
	if request == nil {
		return false, fmt.Errorf("completion has no final validation request")
	}
	var output bytes.Buffer
	if err := runFinalizationCheck(cfg, cfg.RepoRoot, request.Form, &output); err != nil {
		return false, err
	}
	var check finalizationCheckOutput
	if err := json.Unmarshal(output.Bytes(), &check); err != nil {
		return false, err
	}
	result.Steps = append(result.Steps, label+request.Form)
	if check.Status != "ready_for_parent_decision" {
		result.Failure = check
		return false, nil
	}
	return true, nil
}

func writeCompletionWorkflowFailure(stdout io.Writer, result completionWorkflowResult, cause error) error {
	result.Status, result.Failure = "blocked", cause.Error()
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return err
	}
	return cause
}
