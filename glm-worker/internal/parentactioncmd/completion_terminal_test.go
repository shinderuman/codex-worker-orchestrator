package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestCompletionTerminalRequiresTypedObservation(t *testing.T) {
	for _, operation := range []controller.ParentCompletionOperation{{Operation: controller.PublicationOperationObserved}, {Operation: controller.PublicationOperationSuccessorStart}} {
		done, err := completionTerminalBoundary(operation, completionWorkflowResult{})
		if err != nil || !done {
			t.Fatalf("typed completion was not terminal: %v", err)
		}
	}
	diagnostic := controller.ParentCompletionOperation{Operation: controller.PublicationOperationNone, Reason: "accepted candidate is already remotely observed"}
	if done, err := completionTerminalBoundary(diagnostic, completionWorkflowResult{}); err == nil || done {
		t.Fatal("diagnostic text was accepted as publication proof")
	}
}

func TestCompletionHandoffRejectsDifferentControllerAuthority(t *testing.T) {
	result := completionWorkflowResult{
		SourceOID: "published", FinalOID: "published", RemoteOID: "published",
		TransitionID: "publication", ControllerGeneration: 7,
	}
	head := controller.RepositoryControllerHead{
		Status: controller.ControllerStatusActive, ControllerGeneration: 7,
		MetadataLineageRef: &controller.EvidenceObjectRef{LogicalIdentity: "publication"},
		IntegrationTip:     "published", ObservedRemoteTip: "published",
	}
	if err := validateCompletionHandoffHead(result, head); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*controller.RepositoryControllerHead)
	}{
		{"new-task", func(h *controller.RepositoryControllerHead) { h.LiveAttemptID = "new-attempt" }},
		{"changed-lease", func(h *controller.RepositoryControllerHead) { h.LiveLeaseID = "new-lease" }},
		{"changed-status", func(h *controller.RepositoryControllerHead) { h.Status = controller.ControllerStatusFailClosed }},
		{"changed-generation", func(h *controller.RepositoryControllerHead) { h.ControllerGeneration++ }},
		{"different-publication", func(h *controller.RepositoryControllerHead) {
			h.MetadataLineageRef = &controller.EvidenceObjectRef{LogicalIdentity: "another-publication"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := head
			tc.change(&changed)
			if err := validateCompletionHandoffHead(result, changed); err == nil {
				t.Fatal("handoff admitted controller authority outside publication")
			}
		})
	}
}

func TestCompletionHandoffIOFailurePreservesPublicationAndReadOnlyRecovery(t *testing.T) {
	var stdout bytes.Buffer
	terminal := json.RawMessage(`{"status":"completed","final_oid":"published","remote_oid":"published","transition_id":"publication","stop_boundary":"next-task-not-started"}`)
	recovery := &terminalHandoffRecovery{
		Command: []string{"glm-worker", "--handoff", "recovery"}, WorkingDir: t.TempDir(),
	}
	err := finishTerminalEnvelope(config.AppConfig{}, &stdout, terminal, nil, recovery, os.ErrPermission)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("handoff error = %v", err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "parent_action_terminal_handoff_failed" || len(envelope.Handoff) != 0 ||
		!bytes.Equal(envelope.Terminal, terminal) || !reflect.DeepEqual(envelope.HandoffRecovery, recovery) {
		t.Fatalf("handoff failure lost publication or recovery authority: %s", stdout.String())
	}
}

func TestCompletionHandoffOpenFailureRetainsRecoveryCommand(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir(), StateBase: t.TempDir()}
	handoff, recovery, err := readCompletionTerminalHandoff(cfg, completionWorkflowResult{}, nil)
	if err == nil || handoff != nil {
		t.Fatalf("handoff without repository authority: %s, %v", handoff, err)
	}
	var stageErr *completionHandoffStageError
	if !errors.As(err, &stageErr) || stageErr.stage != "repository-identity" {
		t.Fatalf("missing typed publication handoff failure stage: %v", err)
	}
	want := &terminalHandoffRecovery{Command: []string{"glm-worker", "--handoff", "recovery"}, WorkingDir: cfg.RepoRoot}
	if !reflect.DeepEqual(recovery, want) {
		t.Fatalf("handoff open failure lost read-only recovery: %#v", recovery)
	}
}

func TestDirectPublicationHandoffFailurePreservesReadOnlyRecovery(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir(), StateBase: t.TempDir()}
	terminal := json.RawMessage(`{"transition_id":"publication","head":{"metadata_lineage_ref":{"logical_identity":"publication"},"controller_generation":7,"observed_prefix":"published","integration_tip":"published","observed_remote_tip":"published"}}`)
	var stdout bytes.Buffer
	handled, err := finishPublicationTerminalHandoff(cfg, &stdout, nil, terminal)
	if !handled || err == nil {
		t.Fatalf("direct Publication terminal lost its handoff failure: handled=%v error=%v", handled, err)
	}
	var envelope parentActionTerminalEnvelopePayload
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	want := &terminalHandoffRecovery{Command: []string{"glm-worker", "--handoff", "recovery"}, WorkingDir: cfg.RepoRoot}
	if envelope.Status != "parent_action_terminal_handoff_failed" || !bytes.Equal(envelope.Terminal, terminal) ||
		!reflect.DeepEqual(envelope.HandoffRecovery, want) || len(envelope.Handoff) != 0 {
		t.Fatalf("direct Publication lost proof or read-only recovery: %s", stdout.String())
	}
}

func TestIncompletePublicationTerminalDoesNotClaimHandoffSuccess(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: t.TempDir(), StateBase: t.TempDir()}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"status":"blocked","failure":"publication failed"}`),
		json.RawMessage(`{"transition_id":"publication","head":{"controller_generation":7}}`),
	} {
		var stdout bytes.Buffer
		handled, err := finishPublicationTerminalHandoff(cfg, &stdout, nil, raw)
		if handled || err != nil || stdout.Len() != 0 {
			t.Fatalf("non-completed publication entered completion handoff: handled=%v err=%v stdout=%s", handled, err, stdout.String())
		}
	}
}

func TestCompletionTerminalStopsBeforeParentOnlySuccessor(t *testing.T) {
	const generation = 354
	operation := controller.ParentCompletionOperation{
		Operation: controller.PublicationOperationParentOnlyMaterialize,
		Execution: &controller.CompletionExecutionCommand{
			Action:             controller.PublicationOperationParentOnlyMaterialize,
			ExpectedGeneration: generation,
		},
	}
	const source = "1a63fcb4ba21c8daac932b86cde869c3309f54c9"
	published := completionWorkflowResult{
		SourceOID: source, FinalOID: source, RemoteOID: source,
		TransitionID: "b197f785-0204-4e1d-b919-ab156f029906", ControllerGeneration: generation,
	}
	if done, err := completionTerminalBoundary(operation, published); err != nil || !done {
		t.Fatalf("post-publish parent-only successor became a completion error: %v, %v", done, err)
	}
	for _, notPublished := range []completionWorkflowResult{{}, {SourceOID: source, FinalOID: source, RemoteOID: source}} {
		if done, err := completionTerminalBoundary(operation, notPublished); err != nil || done {
			t.Fatalf("parent-only materialization falsely proved publication: %v, %v", done, err)
		}
	}
	const otherOID = "60e1c7e6ce57d1c5af6ac2492f27e60ad81867e5"
	for _, mismatch := range []struct {
		name                 string
		sourceOID, remoteOID string
	}{
		{name: "source-final", sourceOID: otherOID, remoteOID: source},
		{name: "final-remote", sourceOID: source, remoteOID: otherOID},
	} {
		t.Run(mismatch.name, func(t *testing.T) {
			result := published
			result.SourceOID, result.RemoteOID = mismatch.sourceOID, mismatch.remoteOID
			if done, err := completionTerminalBoundary(operation, result); err != nil || done {
				t.Fatalf("mismatched publication OIDs proved completion: %v, %v", done, err)
			}
		})
	}
	operation.Execution.ExpectedGeneration++
	if done, err := completionTerminalBoundary(operation, published); err != nil || done {
		t.Fatalf("stale generation falsely proved completion: %v, %v", done, err)
	}
}
