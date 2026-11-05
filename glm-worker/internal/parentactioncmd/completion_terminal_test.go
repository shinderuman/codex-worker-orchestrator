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

func TestScopeCompletedPublicationHandoff(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"consistent":true,"required_action":"start","action_specs":{"start":{"args":["next"]}},"parent_request":{"continuation":{"state":"continue-now","task":"IMPLEMENTATION_TASKS/next.md","reason":"current-task"}}}`)
	scoped, err := scopeCompletedPublicationHandoff(raw, "IMPLEMENTATION_TASKS/completed.md")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(scoped, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"required_action", "action_specs", "parent_request"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("postpublication response advertised successor action %q: %s", key, scoped)
		}
	}
	var successor struct {
		Task  string `json:"task"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(payload["successor"], &successor); err != nil {
		t.Fatal(err)
	}
	if successor.Task != "IMPLEMENTATION_TASKS/next.md" || successor.State != "not-started" {
		t.Fatalf("successor identity lost: %#v", successor)
	}
	projected, err := projectParentActionTerminalEnvelope(
		json.RawMessage(`{"status":"completed","stop_boundary":"next-task-not-started"}`), scoped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(projected.Handoff, []byte(`"successor"`)) {
		t.Fatalf("projection discarded successor identity: %s", projected.Handoff)
	}
}

func TestScopeCompletedPublicationHandoffPreservesNonSuccessorAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		task string
	}{
		{"same-task", json.RawMessage(`{"consistent":true,"required_action":"controller-publication","parent_request":{"continuation":{"state":"continue-now","task":"current","reason":"active-task-not-started"}}}`), "current"},
		{"blocked", json.RawMessage(`{"consistent":true,"required_action":"recover","parent_request":{"continuation":{"state":"blocked","task":"next","reason":"failure"}}}`), "current"},
		{"current-task-requires-publication", json.RawMessage(`{"consistent":true,"required_action":"controller-publication","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"current-task"}}}`), "current"},
		{"unknown-reason", json.RawMessage(`{"consistent":true,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"failure"}}}`), "current"},
		{"inconsistent", json.RawMessage(`{"consistent":false,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"active-task-not-started"}}}`), "current"},
		{"missing-identity", json.RawMessage(`{"consistent":true,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"active-task-not-started"}}}`), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := scopeCompletedPublicationHandoff(tc.raw, tc.task)
			if err != nil || !bytes.Equal(actual, tc.raw) {
				t.Fatalf("unproven successor authority suppressed: %s %v", actual, err)
			}
		})
	}
	if _, err := scopeCompletedPublicationHandoff(json.RawMessage(`{"consistent":`), "current"); err == nil {
		t.Fatal("malformed handoff silently accepted")
	}
}
