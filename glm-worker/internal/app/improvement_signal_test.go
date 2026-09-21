package app

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffRequiresBoundedImprovementDispositionForInvalidPacket(t *testing.T) {
	callID := "33333333-3333-4333-8333-333333333333"
	required := string(state.ParentActionAccept)
	output := parentHandoffOutput{
		RequiredAction: &required,
		AllowedActions: []string{string(state.ParentActionAccept)},
		LastMaterial: &parentHandoffMaterial{
			CallID:             &callID,
			CallType:           state.CallTypeTask,
			Outcome:            "invalid_packet",
			PacketRejectReason: "schema-invalid",
		},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		RequiredAction           string                             `json:"required_action"`
		AllowedActions           []string                           `json:"allowed_actions"`
		RequiredActionParameters map[string]string                  `json:"required_action_parameters"`
		ActionSpecs              map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	action := string(state.ParentActionImprovementDisposition)
	if decoded.RequiredAction != action || len(decoded.AllowedActions) != 1 || decoded.AllowedActions[0] != action {
		t.Fatalf("projected action = %q %#v", decoded.RequiredAction, decoded.AllowedActions)
	}
	if decoded.RequiredActionParameters[improvementSignalKindParameter] != state.ImprovementSignalInvalidPacket || decoded.RequiredActionParameters[improvementSignalCallIDParameter] != callID || decoded.RequiredActionParameters[improvementSignalReasonParameter] != "schema-invalid" {
		t.Fatalf("signal parameters = %#v", decoded.RequiredActionParameters)
	}
	spec, ok := decoded.ActionSpecs[action]
	if !ok || spec.Kind != "bounded-choice" {
		t.Fatalf("action spec = %#v", spec)
	}
	if len(spec.Command) != 6 ||
		spec.Command[0] != "glm-parent-action" ||
		spec.Command[1] != action ||
		spec.Command[2] != "--signal-kind" ||
		spec.Command[3] != state.ImprovementSignalInvalidPacket ||
		spec.Command[4] != "--source-call-id" ||
		spec.Command[5] != callID {
		t.Fatalf("command = %#v", spec.Command)
	}
	choices := spec.Choices["--disposition"]
	if len(choices) != 5 {
		t.Fatalf("choices = %#v", choices)
	}
}

func TestParentHandoffDoesNotInventImprovementSignalFromNormalMaterial(t *testing.T) {
	required := string(state.ParentActionAccept)
	output := parentHandoffOutput{
		RequiredAction: &required,
		AllowedActions: []string{string(state.ParentActionAccept)},
		LastMaterial:   &parentHandoffMaterial{CallType: state.CallTypeTask, Outcome: "success"},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		RequiredAction string `json:"required_action"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("normal handoff action changed to %q", decoded.RequiredAction)
	}
}

func TestParentHandoffDoesNotProjectSignalWithoutStableCallID(t *testing.T) {
	required := string(state.ParentActionAccept)
	output := parentHandoffOutput{
		RequiredAction: &required,
		AllowedActions: []string{string(state.ParentActionAccept)},
		LastMaterial: &parentHandoffMaterial{
			CallType:           state.CallTypeTask,
			Outcome:            "invalid_packet",
			PacketRejectReason: "schema-invalid",
		},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		RequiredAction string                             `json:"required_action"`
		ActionSpecs    map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("unbound signal changed required action to %q", decoded.RequiredAction)
	}
	if _, ok := decoded.ActionSpecs[string(state.ParentActionImprovementDisposition)]; ok {
		t.Fatalf("unbound signal projected disposition action: %#v", decoded.ActionSpecs)
	}
}
