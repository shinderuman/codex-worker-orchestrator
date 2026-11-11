package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

type terminalHandoffRecovery struct {
	Command    []string `json:"command"`
	WorkingDir string   `json:"working_dir"`
}

type completionHandoffStageError struct {
	stage string
	cause error
}

type completedPublicationHandoff struct {
	Version        json.RawMessage   `json:"version"`
	Consistent     bool              `json:"consistent"`
	TaskID         string            `json:"task_id"`
	TaskStatus     string            `json:"task_status"`
	RequiredAction string            `json:"required_action"`
	Parameters     map[string]string `json:"required_action_parameters"`
	ParentRequest  struct {
		Continuation repositoryproject.Continuation `json:"continuation"`
	} `json:"parent_request"`
}

func (e *completionHandoffStageError) Error() string {
	return e.cause.Error()
}

func (e *completionHandoffStageError) Unwrap() error {
	return e.cause
}

func completionHandoffFailure(stage string, err error) error {
	return &completionHandoffStageError{stage: stage, cause: err}
}

func finishPublicationTerminalHandoff(cfg config.AppConfig, stdout, stderr io.Writer, terminal json.RawMessage) (bool, error) {
	result, err := publicationTerminalCompletion(terminal)
	if err != nil || result.Status != completionWorkflowCompleted {
		return false, err
	}
	handoff, recovery, err := readCompletionTerminalHandoff(cfg, result, stderr)
	return true, finishTerminalEnvelope(cfg, stdout, terminal, handoff, recovery, err)
}

func publicationTerminalCompletion(terminal json.RawMessage) (completionWorkflowResult, error) {
	var output struct {
		completionWorkflowResult
		Head *controller.RepositoryControllerHead `json:"head"`
	}
	if err := json.Unmarshal(terminal, &output); err != nil {
		return completionWorkflowResult{}, err
	}
	result := output.completionWorkflowResult
	head := output.Head
	if result.Status != "" || result.TransitionID == "" || head == nil ||
		head.MetadataLineageRef == nil || head.MetadataLineageRef.LogicalIdentity != result.TransitionID {
		return result, nil
	}
	result.Status = completionWorkflowCompleted
	result.SourceOID = head.ObservedPrefix
	result.FinalOID = head.IntegrationTip
	result.RemoteOID = head.ObservedRemoteTip
	result.ControllerGeneration = head.ControllerGeneration
	return result, nil
}

func readCompletionTerminalHandoff(cfg config.AppConfig, result completionWorkflowResult, stderr io.Writer) (json.RawMessage, *terminalHandoffRecovery, error) {
	recovery := &terminalHandoffRecovery{
		Command:    []string{"glm-worker", "--handoff", "recovery"},
		WorkingDir: cfg.RepoRoot,
	}
	identity, err := controller.ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return nil, recovery, completionHandoffFailure("repository-identity", err)
	}
	cfg.RepoRoot = identity.PrimaryRoot
	recovery.WorkingDir = cfg.RepoRoot
	store, err := controller.Open(cfg)
	if err != nil {
		return nil, recovery, completionHandoffFailure("controller-open", err)
	}
	expected, err := store.LoadHead()
	if err != nil {
		return nil, recovery, completionHandoffFailure("controller-head-read", err)
	}
	if err := validateCompletionHandoffHead(result, expected); err != nil {
		return nil, recovery, completionHandoffFailure("published-head-validation", err)
	}
	handoff, err := readTerminalHandoff(cfg, stderr, false)
	if err != nil {
		return nil, recovery, completionHandoffFailure("canonical-handoff-process", err)
	}
	if err := validateCompletionHandoffProjection(store, expected, handoff); err != nil {
		return nil, recovery, completionHandoffFailure("canonical-handoff-validation", err)
	}
	terminalRecord, err := store.LoadTerminalTaskRecord(*expected.MetadataLineageRef)
	if err != nil {
		return nil, recovery, err
	}
	handoff, err = scopeCompletedPublicationHandoff(handoff, terminalRecord.TaskRef.TaskPath, expected)
	if err != nil {
		return nil, recovery, err
	}
	return handoff, nil, nil
}

func validateCompletionHandoffHead(result completionWorkflowResult, head controller.RepositoryControllerHead) error {
	if !hasCompletionPublicationEvidence(result) {
		return fmt.Errorf("completion handoff lacks finalized publication evidence")
	}
	if head.ControllerGeneration != result.ControllerGeneration || head.MetadataLineageRef == nil ||
		head.MetadataLineageRef.LogicalIdentity != result.TransitionID || head.Status != controller.ControllerStatusActive ||
		head.IntegrationTip != result.FinalOID || head.ObservedRemoteTip != result.RemoteOID ||
		head.PendingTransitionID != "" || head.LiveAttemptID != "" || head.LiveLeaseID != "" {
		return fmt.Errorf("controller authority changed after publication")
	}
	return nil
}

func hasCompletionPublicationEvidence(result completionWorkflowResult) bool {
	return result.TransitionID != "" && result.ControllerGeneration != 0 && result.FinalOID != "" &&
		result.SourceOID == result.FinalOID && result.RemoteOID == result.FinalOID
}

func validateCompletionHandoffProjection(store *controller.Store, expected controller.RepositoryControllerHead, raw json.RawMessage) error {
	var handoff struct {
		Consistent bool                               `json:"consistent"`
		Controller *controller.ControllerStatusReport `json:"controller"`
	}
	if err := json.Unmarshal(raw, &handoff); err != nil {
		return err
	}
	if !handoff.Consistent || handoff.Controller == nil || !handoff.Controller.CanonicalActive ||
		!reflect.DeepEqual(expected, handoff.Controller.Head) {
		return fmt.Errorf("canonical handoff does not match finalized publication authority")
	}
	current, err := store.LoadHead()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, current) {
		return fmt.Errorf("controller authority changed during completion handoff")
	}
	return nil
}

func scopeCompletedPublicationHandoff(raw json.RawMessage, completedTask string, head controller.RepositoryControllerHead) (json.RawMessage, error) {
	var canonical completedPublicationHandoff
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, fmt.Errorf("decode verified completion handoff: %w", err)
	}
	successor := completedPublicationSuccessor(canonical, completedTask, head)
	if successor == "" {
		return raw, nil
	}
	return json.Marshal(struct {
		Version    json.RawMessage `json:"version,omitempty"`
		Consistent bool            `json:"consistent"`
		Successor  struct {
			Task  string `json:"task"`
			State string `json:"state"`
		} `json:"successor"`
	}{
		Version:    canonical.Version,
		Consistent: true,
		Successor: struct {
			Task  string `json:"task"`
			State string `json:"state"`
		}{Task: successor, State: "not-started"},
	})
}

func completedPublicationSuccessor(handoff completedPublicationHandoff, completedTask string, head controller.RepositoryControllerHead) string {
	if !handoff.Consistent || completedTask == "" || head.RootTaskRef == nil ||
		head.ExecutionTaskRef != nil || head.ActiveEpisodeID != "" {
		return ""
	}
	successor := head.RootTaskRef.TaskPath
	if successor == "" || successor == completedTask {
		return ""
	}
	if isCompletedPublicationSuccessor(handoff, successor) || isParentOnlySuccessorCommand(handoff, head.ControllerGeneration) {
		return successor
	}
	return ""
}

func isCompletedPublicationSuccessor(handoff completedPublicationHandoff, successor string) bool {
	continuation := handoff.ParentRequest.Continuation
	if continuation.State != repositoryproject.ContinuationContinueNow || continuation.Task != successor ||
		handoff.Parameters["controller_command_json"] != "" {
		return false
	}
	switch continuation.Reason {
	case repositoryproject.ReasonActiveTaskNotStarted, repositoryproject.ReasonPostCompletionActive:
		return publicationSuccessorStart(handoff)
	case repositoryproject.ReasonCurrentTask:
		return publicationSuccessorStart(handoff) || handoff.RequiredAction == controller.PublicationOperationSurface &&
			handoff.TaskID == "" && handoff.TaskStatus == ""
	default:
		return false
	}
}

func publicationSuccessorStart(handoff completedPublicationHandoff) bool {
	action := handoff.ParentRequest.Continuation.RequiredAction
	return handoff.RequiredAction == controller.SuccessorStartSurface &&
		(action == "" || action == controller.SuccessorStartSurface)
}

func isParentOnlySuccessorCommand(handoff completedPublicationHandoff, generation uint64) bool {
	if handoff.RequiredAction != controller.ExecutionOperationSurface {
		return false
	}
	var command controller.CompletionExecutionCommand
	if err := json.Unmarshal([]byte(handoff.Parameters["controller_command_json"]), &command); err != nil {
		return false
	}
	return command.Action == controller.PublicationOperationParentOnlyMaterialize && command.ExpectedGeneration == generation
}
