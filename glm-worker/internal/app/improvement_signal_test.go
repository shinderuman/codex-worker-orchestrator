package app

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffProjectsBoundedImprovementAdvisoryWithoutReplacingLifecycleAction(t *testing.T) {
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
		ImprovementSignal        *parentHandoffImprovementSignal    `json:"improvement_signal"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequiredAction != string(state.ParentActionAccept) || len(decoded.AllowedActions) != 1 || decoded.AllowedActions[0] != string(state.ParentActionAccept) {
		t.Fatalf("lifecycle action changed = %q %#v", decoded.RequiredAction, decoded.AllowedActions)
	}
	if len(decoded.RequiredActionParameters) != 0 {
		t.Fatalf("lifecycle parameters were replaced by advisory signal: %#v", decoded.RequiredActionParameters)
	}
	if _, ok := decoded.ActionSpecs[string(state.ParentActionAccept)]; !ok {
		t.Fatalf("canonical lifecycle action spec missing: %#v", decoded.ActionSpecs)
	}
	if _, ok := decoded.ActionSpecs[string(state.ParentActionImprovementDisposition)]; ok {
		t.Fatalf("advisory action leaked into lifecycle action specs: %#v", decoded.ActionSpecs)
	}
	if decoded.ImprovementSignal == nil {
		t.Fatal("improvement advisory was not projected")
	}
	if decoded.ImprovementSignal.Signal.Kind != state.ImprovementSignalInvalidPacket || decoded.ImprovementSignal.Signal.SourceCallID != callID || decoded.ImprovementSignal.Signal.Reason != "schema-invalid" {
		t.Fatalf("signal = %#v", decoded.ImprovementSignal.Signal)
	}
	spec := decoded.ImprovementSignal.ActionSpec
	action := string(state.ParentActionImprovementDisposition)
	if spec.Kind != "bounded-choice" {
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
		RequiredAction    string                          `json:"required_action"`
		ImprovementSignal *parentHandoffImprovementSignal `json:"improvement_signal"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("normal handoff action changed to %q", decoded.RequiredAction)
	}
	if decoded.ImprovementSignal != nil {
		t.Fatalf("normal material projected improvement signal: %#v", decoded.ImprovementSignal)
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
		RequiredAction    string                          `json:"required_action"`
		ImprovementSignal *parentHandoffImprovementSignal `json:"improvement_signal"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("unbound signal changed required action to %q", decoded.RequiredAction)
	}
	if decoded.ImprovementSignal != nil {
		t.Fatalf("unbound signal projected advisory: %#v", decoded.ImprovementSignal)
	}
}
