package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newAwaitingParentCompletionHandoff(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	return cfg, st
}

func handoffAllowsAction(actions []string, action string) bool {
	for _, value := range actions {
		if value == action {
			return true
		}
	}
	return false
}

func TestParentHandoffAwaitingParentCompletionProjectsTaskStatus(t *testing.T) {
	_, st := newAwaitingParentCompletionHandoff(t)

	output := buildParentHandoff(st)
	if !output.Consistent {
		t.Fatalf("awaiting handoff inconsistent: %#v", output)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusAwaitingParentCompletion) {
		t.Fatalf("awaiting handoff task status = %#v", output.TaskStatus)
	}
	if !handoffAllowsAction(output.AllowedActions, string(state.ParentActionReopen)) {
		t.Fatalf("awaiting handoff allowed actions = %#v", output.AllowedActions)
	}
	assertHandoffJSONExposesReopenSpec(t, output)
	recovery := projectParentHandoffRecovery(output)
	if recovery.TaskStatus == nil || *recovery.TaskStatus != string(state.TaskStatusAwaitingParentCompletion) {
		t.Fatalf("awaiting recovery handoff task status = %#v", recovery.TaskStatus)
	}
	assertHandoffJSONExposesReopenSpec(t, recovery)
}

func TestParentHandoffAfterReopenProjectsWaitingSolReviewWithFixAction(t *testing.T) {
	_, st := newAwaitingParentCompletionHandoff(t)
	if err := st.ReopenAcceptedParentCompletion(state.ParentOriginCodexReview, state.ParentCauseProductionWiring); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if !output.Consistent {
		t.Fatalf("reopened handoff inconsistent: %#v", output)
	}
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("reopened handoff task status = %#v", output.TaskStatus)
	}
	if output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("reopened required action = %#v", output.RequiredAction)
	}
	if !handoffAllowsAction(output.AllowedActions, string(state.ParentActionFix)) {
		t.Fatalf("reopened allowed actions lack fix: %#v", output.AllowedActions)
	}
	if handoffAllowsAction(output.AllowedActions, string(state.ParentActionAccept)) || handoffAllowsAction(output.AllowedActions, string(state.ParentActionComplete)) {
		t.Fatalf("reopened allowed actions keep terminal actions: %#v", output.AllowedActions)
	}
	if output.Publication != nil {
		t.Fatalf("reopened handoff keeps publication projection: %#v", output.Publication)
	}
	recovery := projectParentHandoffRecovery(output)
	if recovery.TaskStatus == nil || *recovery.TaskStatus != string(state.TaskStatusWaitingSolReview) {
		t.Fatalf("reopened recovery handoff task status = %#v", recovery.TaskStatus)
	}
	if !handoffAllowsAction(recovery.AllowedActions, string(state.ParentActionFix)) {
		t.Fatalf("reopened recovery allowed actions = %#v", recovery.AllowedActions)
	}
}

func assertHandoffJSONExposesReopenSpec(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ActionSpecs map[string]struct {
			Kind    string   `json:"kind"`
			Command []string `json:"command"`
		} `json:"action_specs"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	spec, ok := decoded.ActionSpecs[string(state.ParentActionReopen)]
	if !ok || spec.Kind != "direct" {
		t.Fatalf("handoff JSON lacks a direct reopen action spec: %s", data)
	}
	want := []string{"glm-parent-action", "reopen"}
	if !reflect.DeepEqual(spec.Command, want) {
		t.Fatalf("reopen action spec command = %#v want %#v", spec.Command, want)
	}
}
