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

func TestScopeCompletedPublicationHandoff(t *testing.T) {
	raw := json.RawMessage(`{"version":1,"consistent":true,"required_action":"start","action_specs":{"start":{"args":["next"]}},"parent_request":{"continuation":{"state":"continue-now","task":"IMPLEMENTATION_TASKS/next.md","reason":"current-task"}}}`)
	scoped, err := scopeCompletedPublicationHandoff(raw, "IMPLEMENTATION_TASKS/completed.md", controller.RepositoryControllerHead{RootTaskRef: &controller.SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/next.md"}})
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
		{"current-task-requires-publication", json.RawMessage(`{"consistent":true,"task_id":"still-active","task_status":"awaiting-parent-completion","required_action":"controller-publication","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"current-task"}}}`), "current"},
		{"unknown-reason", json.RawMessage(`{"consistent":true,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"failure"}}}`), "current"},
		{"inconsistent", json.RawMessage(`{"consistent":false,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"active-task-not-started"}}}`), "current"},
		{"missing-identity", json.RawMessage(`{"consistent":true,"required_action":"recover","parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"active-task-not-started"}}}`), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := scopeCompletedPublicationHandoff(tc.raw, tc.task, controller.RepositoryControllerHead{RootTaskRef: &controller.SemanticTaskRef{TaskPath: "next"}})
			if err != nil || !bytes.Equal(actual, tc.raw) {
				t.Fatalf("unproven successor authority suppressed: %s %v", actual, err)
			}
		})
	}
	if _, err := scopeCompletedPublicationHandoff(json.RawMessage(`{"consistent":`), "current", controller.RepositoryControllerHead{RootTaskRef: &controller.SemanticTaskRef{TaskPath: "next"}}); err == nil {
		t.Fatal("malformed handoff silently accepted")
	}
}

func TestScopeCompletedPublicationHandoffParentOnlySuccessor(t *testing.T) {
	head := controller.RepositoryControllerHead{
		ControllerGeneration: 354,
		RootTaskRef:          &controller.SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/eval.md"},
	}
	raw := json.RawMessage(`{"consistent":true,"required_action":"controller-execution","required_action_parameters":{"controller_command_json":"{\"action\":\"materialize-parent-only\",\"expected_generation\":354}"},"parent_request":null}`)
	scoped, err := scopeCompletedPublicationHandoff(raw, "IMPLEMENTATION_TASKS/completed.md", head)
	if err != nil || !bytes.Contains(scoped, []byte(`"successor":{"task":"IMPLEMENTATION_TASKS/eval.md","state":"not-started"}`)) || bytes.Contains(scoped, []byte(`"required_action"`)) {
		t.Fatalf("parent-only successor was not reduced to observation: %s %v", scoped, err)
	}
	head.ControllerGeneration++
	scoped, err = scopeCompletedPublicationHandoff(raw, "IMPLEMENTATION_TASKS/completed.md", head)
	if err != nil || !bytes.Equal(scoped, raw) {
		t.Fatalf("stale successor command lost its original evidence: %s %v", scoped, err)
	}
}

func TestScopeCompletedPublicationHandoffAudit1410(t *testing.T) {
	const completed = "IMPLEMENTATION_TASKS/completion-required-validation-closure.md"
	const next = "IMPLEMENTATION_TASKS/task-resource-observability-bundle-closure.md"
	raw := json.RawMessage(`{"consistent":true,"task_id":null,"task_status":null,"required_action":"controller-publication","action_specs":{"controller-publication":{"prepare_command":["glm-parent-action","prepare","controller-publication"]},"controller-evidence":{"prepare_command":["glm-parent-action","prepare","controller-evidence"]}},"parent_request":{"continuation":{"state":"continue-now","task":"` + next + `","reason":"current-task","required_action":"controller-publication"}}}`)
	head := controller.RepositoryControllerHead{RootTaskRef: &controller.SemanticTaskRef{TaskPath: next}}
	scoped, err := scopeCompletedPublicationHandoff(raw, completed, head)
	if err != nil || string(scoped) != `{"consistent":true,"successor":{"task":"`+next+`","state":"not-started"}}` {
		t.Fatalf("Audit #1410 retained publication actions after the completed task: %s %v", scoped, err)
	}
	head.ActiveEpisodeID = "retained-child-episode"
	scoped, err = scopeCompletedPublicationHandoff(raw, completed, head)
	if err != nil || !bytes.Equal(scoped, raw) {
		t.Fatalf("active episode recovery was suppressed: %s %v", scoped, err)
	}
}

func TestScopeCompletedPublicationHandoffKeepsExplicitControllerOperation(t *testing.T) {
	head := controller.RepositoryControllerHead{
		ControllerGeneration: 42,
		RootTaskRef:          &controller.SemanticTaskRef{TaskPath: "next"},
	}
	for _, scenario := range []struct {
		reason  string
		action  string
		command string
	}{
		{"active-task-not-started", "controller-execution", "sync-task-authority"},
		{"post-local-completion-active", "controller-publication", "rebind"},
		{"active-task-not-started", "start", "sync-task-authority"},
	} {
		t.Run(scenario.reason+"/"+scenario.action, func(t *testing.T) {
			raw := json.RawMessage(`{"consistent":true,"required_action":"` + scenario.action +
				`","required_action_parameters":{"controller_command_json":"{\"action\":\"` + scenario.command +
				`\",\"expected_generation\":42}"},"parent_request":{"continuation":{"state":"continue-now","task":"next","reason":"` +
				scenario.reason + `"}}}`)
			projected, err := scopeCompletedPublicationHandoff(raw, "completed", head)
			if err != nil || !bytes.Equal(projected, raw) {
				t.Fatalf("explicit controller operation was discarded: %s %v", projected, err)
			}
		})
	}
}


func TestCompletionTerminalCompletedRequestRequiresMatchingPublicationAndOwner(t *testing.T) {
	const oid = "1a63fcb4ba21c8daac932b86cde869c3309f54c9"
	receipt := &controller.CompletedRequestPublication{
		RequestID: "request-one", FinalOID: oid, RemoteOID: oid,
		TransitionID: "publication-one", ControllerGeneration: 42,
	}
	operation := controller.ParentCompletionOperation{
		Operation: controller.PublicationOperationNone,
		CompletedRequest: receipt,
	}
	published := completionWorkflowResult{
		RequestID: "request-one", SourceOID: oid, FinalOID: oid, RemoteOID: oid,
		TransitionID: "publication-one", ControllerGeneration: 42,
	}
	if done, err := completionTerminalBoundary(operation, published); err != nil || !done {
		t.Fatalf("matching published request was not terminal: done=%v err=%v", done, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*completionWorkflowResult)
	}{
		{"missing-publication", func(r *completionWorkflowResult) { *r = completionWorkflowResult{} }},
		{"different-request", func(r *completionWorkflowResult) { r.RequestID = "request-two" }},
		{"missing-request", func(r *completionWorkflowResult) { r.RequestID = "" }},
		{"different-source", func(r *completionWorkflowResult) { r.SourceOID = "other" }},
		{"missing-final", func(r *completionWorkflowResult) { r.FinalOID = "" }},
		{"different-final", func(r *completionWorkflowResult) { r.FinalOID = "other" }},
		{"different-remote", func(r *completionWorkflowResult) { r.RemoteOID = "other" }},
		{"different-transition", func(r *completionWorkflowResult) { r.TransitionID = "other" }},
		{"missing-transition", func(r *completionWorkflowResult) { r.TransitionID = "" }},
		{"different-generation", func(r *completionWorkflowResult) { r.ControllerGeneration++ }},
		{"missing-generation", func(r *completionWorkflowResult) { r.ControllerGeneration = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := published
			tc.change(&result)
			if done, err := completionTerminalBoundary(operation, result); done || err == nil {
				t.Fatalf("unproven request was accepted: done=%v err=%v", done, err)
			}
		})
	}
	operation.CompletedRequest = nil
	if done, err := completionTerminalBoundary(operation, published); done || err == nil {
		t.Fatalf("unwitnessed none operation was accepted: done=%v err=%v", done, err)
	}
	operation.CompletedRequest = &controller.CompletedRequestPublication{RequestID: "request-two", FinalOID: oid, RemoteOID: oid, TransitionID: "publication-one", ControllerGeneration: 42}
	if done, err := completionTerminalBoundary(operation, published); done || err == nil {
		t.Fatalf("unmatched receipt was accepted: done=%v err=%v", done, err)
	}
}
