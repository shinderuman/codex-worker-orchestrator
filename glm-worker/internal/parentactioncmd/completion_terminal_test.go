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
		done, err := completionTerminalBoundary(operation)
		if err != nil || !done {
			t.Fatalf("typed completion was not terminal: %v", err)
		}
	}
	diagnostic := controller.ParentCompletionOperation{Operation: controller.PublicationOperationNone, Reason: "accepted candidate is already remotely observed"}
	if done, err := completionTerminalBoundary(diagnostic); err == nil || done {
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
