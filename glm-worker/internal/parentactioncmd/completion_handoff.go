package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

type terminalHandoffRecovery struct {
	Command    []string `json:"command"`
	WorkingDir string   `json:"working_dir"`
}

func readCompletionTerminalHandoff(cfg config.AppConfig, result completionWorkflowResult, stderr io.Writer) (json.RawMessage, *terminalHandoffRecovery, error) {
	recovery := &terminalHandoffRecovery{
		Command:    []string{"glm-worker", "--handoff", "recovery"},
		WorkingDir: cfg.RepoRoot,
	}
	identity, err := controller.ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return nil, recovery, err
	}
	cfg.RepoRoot = identity.PrimaryRoot
	recovery.WorkingDir = cfg.RepoRoot
	store, err := controller.Open(cfg)
	if err != nil {
		return nil, recovery, err
	}
	expected, err := store.LoadHead()
	if err != nil {
		return nil, recovery, err
	}
	if err := validateCompletionHandoffHead(result, expected); err != nil {
		return nil, recovery, err
	}
	handoff, err := readTerminalHandoff(cfg, stderr, false)
	if err != nil {
		return nil, recovery, err
	}
	if err := validateCompletionHandoffProjection(store, expected, handoff); err != nil {
		return nil, recovery, err
	}
	// The canonical handoff describes the successor, not an action required
	// to finish the already published task. Only remove successor actions
	// after the source-bound publication and controller authority checks.
	terminalRecord, err := store.LoadTerminalTaskRecord(*expected.MetadataLineageRef)
	if err != nil {
		return nil, recovery, err
	}
	handoff, err = scopeCompletedPublicationHandoff(handoff, terminalRecord.TaskRef.TaskPath)
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

// scopeCompletedPublicationHandoff prevents a completed publication's terminal
// response from advertising the *next* task as an action for the completed
// task. A fresh canonical handoff remains the authority when that successor
// is explicitly started. An incomplete or ambiguous handoff is left intact.
func scopeCompletedPublicationHandoff(raw json.RawMessage, completedTask string) (json.RawMessage, error) {
	var canonical struct {
		Version       json.RawMessage `json:"version"`
		Consistent    bool            `json:"consistent"`
		RequiredAction string         `json:"required_action"`
		ParentRequest struct {
			Continuation struct {
				State  string `json:"state"`
				Task   string `json:"task"`
				Reason string `json:"reason"`
			} `json:"continuation"`
		} `json:"parent_request"`
	}
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return nil, fmt.Errorf("decode verified completion handoff: %w", err)
	}
	continuation := canonical.ParentRequest.Continuation
	if !canonical.Consistent || completedTask == "" ||
		continuation.State != "continue-now" || continuation.Task == "" ||
		continuation.Task == completedTask ||
		(continuation.Reason != "active-task-not-started" &&
			continuation.Reason != "post-local-completion-active" &&
			!(continuation.Reason == "current-task" && canonical.RequiredAction == "start")) {
		return raw, nil
	}
	// Only the successor identity is relevant at the completed-task boundary.
	// In particular do not expose required_action/action_specs/parent_request,
	// which belong to a separate, unstarted task. Do not infer permission to
	// start that task from publication success.
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
		}{Task: continuation.Task, State: "not-started"},
	})
}
