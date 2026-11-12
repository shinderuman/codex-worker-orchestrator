package app

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type controllerExecutionProjection struct {
	Consistent     bool              `json:"consistent"`
	RequiredAction *string           `json:"required_action"`
	Parameters     map[string]string `json:"required_action_parameters,omitempty"`
	PrepareCommand []string          `json:"prepare_command,omitempty"`
	Inconsistency  string            `json:"inconsistency,omitempty"`
}

func projectResolvedBlockerExecution(cfg config.AppConfig, result *controller.FindingDispositionResult) *controllerExecutionProjection {
	if result == nil || result.Episode == nil || result.NextTaskRef == nil {
		return nil
	}
	episode := result.Episode
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	projection, err := projectRequestedExecutionOperation(cfg, handoff, []string{
		"--operation", "suspend", episode.EpisodeID, strconv.FormatUint(episode.Revision, 10),
	})
	if err != nil {
		return &controllerExecutionProjection{Consistent: false, Inconsistency: err.Error()}
	}
	return &projection
}

func runControllerExecutionProjection(args []string, loadConfig func() (config.AppConfig, error), stdout io.Writer) (bool, error) {
	if len(args) < 2 || args[0] != controllerAuthorityFlag || args[1] != "controller-execution-plan" {
		return false, nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return true, err
	}
	cfg, err = controller.WorkflowConfig(cfg)
	if err != nil {
		return true, err
	}
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	if len(args) > 2 {
		projection, err := projectRequestedExecutionOperation(cfg, handoff, args[2:])
		if err != nil {
			return true, err
		}
		return true, writeValidatedMachineJSON(stdout, projection)
	}
	projection := controllerExecutionProjection{Consistent: handoff.Consistent, RequiredAction: handoff.RequiredAction}
	if handoff.RequiredAction != nil && *handoff.RequiredAction == controllerExecutionSurface {
		projection.Parameters = handoff.RequiredActionParameters
	}
	return true, writeValidatedMachineJSON(stdout, projection)
}

func projectRequestedExecutionOperation(cfg config.AppConfig, handoff parentHandoffOutput, options []string) (controllerExecutionProjection, error) {
	if !parentactiongrammar.ValidExecutionOperationSelection(options) {
		return controllerExecutionProjection{}, fmt.Errorf("unsupported execution operation selection")
	}
	if !requestedExecutionFollowsHandoff(handoff, options[1]) {
		return controllerExecutionProjection{}, fmt.Errorf("execution operation %s cannot replace the current handoff action", options[1])
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return controllerExecutionProjection{}, err
	}
	command, err := requestedExecutionCommand(cfg, store, handoff.Controller.Head, options)
	if err != nil {
		return controllerExecutionProjection{}, err
	}
	if command.ExpectedGeneration != handoff.Controller.Head.ControllerGeneration {
		return controllerExecutionProjection{}, fmt.Errorf("execution operation was projected against a different handoff generation")
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return controllerExecutionProjection{}, err
	}
	action := controllerExecutionSurface
	parameters := map[string]string{"controller_command_json": string(raw)}
	if command.ExecutionTermination != nil {
		parameters["semantic_slot"] = "execution_termination.reason"
	}
	return controllerExecutionProjection{
		Consistent: true, RequiredAction: &action,
		Parameters:     parameters,
		PrepareCommand: append([]string{"glm-parent-action", "prepare", action}, options...),
	}, nil
}

func requestedExecutionFollowsHandoff(handoff parentHandoffOutput, action string) bool {
	if !handoff.Consistent || handoff.RequiredAction == nil || handoff.Controller == nil || handoff.Controller.Head.PendingTransitionID != "" {
		return false
	}
	command := handoff.RequiredActionParameters["controller_command_json"]
	if action == "user-terminate-execution" {
		return terminationFollowsCompletion(*handoff.RequiredAction, command)
	}
	if command != "" {
		return false
	}
	switch *handoff.RequiredAction {
	case canonicalStartAction, controllerExecutionSurface, string(state.ParentActionNone):
		return true
	case string(state.ParentActionResume), string(state.ParentActionReview), string(state.ParentActionFix):
		return action == "suspend"
	default:
		return false
	}
}

func terminationFollowsCompletion(requiredAction, command string) bool {
	var publication controllerPublicationCommand
	return requiredAction == controllerPublicationSurface && json.Unmarshal([]byte(command), &publication) == nil &&
		(publication.Action == controller.PublicationOperationAccept || publication.Action == controller.PublicationOperationReopen)
}

func requestedExecutionCommand(cfg config.AppConfig, store *controller.Store, head controller.RepositoryControllerHead, options []string) (workflow.ControllerExecutionCommand, error) {
	command := workflow.ControllerExecutionCommand{Action: options[1]}
	if command.Action == "user-terminate-execution" {
		input, err := previewAdoptedExecutionTermination(store, head)
		command.ExpectedGeneration, command.ExecutionTermination = input.ExpectedGeneration, &input
		return command, err
	}
	id := options[2]
	if digest, err := hex.DecodeString(id); err != nil || len(digest) != 32 {
		return command, fmt.Errorf("execution projection requires an exact operation identity")
	}
	switch command.Action {
	case "suspend":
		revision, err := strconv.ParseUint(options[3], 10, 64)
		if err != nil || revision == 0 || strconv.FormatUint(revision, 10) != options[3] {
			return command, fmt.Errorf("execution suspension requires an exact episode revision")
		}
		command.EpisodeID, command.EpisodeRevision = id, revision
		command.ExpectedGeneration, err = store.PreviewExecutionSuspension(cfg, id, revision)
		return command, err
	case "gc-suspension":
		command.SuspensionID = id
		generation, err := store.PreviewSuspensionGC(id)
		command.ExpectedGeneration = generation
		return command, err
	default:
		return command, fmt.Errorf("unsupported execution projection operation or operands")
	}
}

func previewAdoptedExecutionTermination(store *controller.Store, head controller.RepositoryControllerHead) (controller.UserExecutionTransitionInput, error) {
	input, err := store.PreviewExecutionTermination()
	if err != nil {
		return input, err
	}
	if input.SuccessorProjectID != head.ProjectSnapshotID || head.IntegrationTip != head.ObservedRemoteTip {
		return input, fmt.Errorf("execution termination requires an adopted successor source")
	}
	return input, nil
}
