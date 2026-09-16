package parentaction

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPreparedProjection(t *testing.T) {
	prepared := Prepared{Action: string(ActionDecision), Token: "0123456789abcdef0123456789abcdef", Path: "/tmp/decision.txt"}
	raw, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Status      string         `json:"status"`
		Slots       []PreparedSlot `json:"slots"`
		NextCommand []string       `json:"next_command"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "prepared" {
		t.Fatalf("status = %q want prepared", decoded.Status)
	}
	if !reflect.DeepEqual(decoded.NextCommand, []string{"glm-parent-action", "decision", prepared.Token}) {
		t.Fatalf("next command = %#v", decoded.NextCommand)
	}
	want := []PreparedSlot{
		{Name: "decision", Placeholder: decisionPlaceholder},
		{Name: "execution_unit", Placeholder: executionUnitPlaceholder},
		{Name: "milestones_json", Placeholder: `{"milestones":[]}`, RequiredWhen: "execution_unit=milestones"},
	}
	if !reflect.DeepEqual(decoded.Slots, want) {
		t.Fatalf("slots = %#v want %#v", decoded.Slots, want)
	}
}

func TestPreparedProjectionFixExposesPayloadSlotAndNextCommand(t *testing.T) {
	prepared := Prepared{Action: string(ActionFix), Token: "fedcba9876543210fedcba9876543210", Path: "/tmp/fix.txt"}
	raw, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Status      string         `json:"status"`
		Slots       []PreparedSlot `json:"slots"`
		NextCommand []string       `json:"next_command"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != "prepared" {
		t.Fatalf("status = %q want prepared", decoded.Status)
	}
	if !reflect.DeepEqual(decoded.NextCommand, []string{"glm-parent-action", "fix", prepared.Token}) {
		t.Fatalf("next command = %#v", decoded.NextCommand)
	}
	want := []PreparedSlot{{Name: "payload", Placeholder: "__GLM_PARENT_ACTION_PAYLOAD__"}}
	if !reflect.DeepEqual(decoded.Slots, want) {
		t.Fatalf("slots = %#v want %#v", decoded.Slots, want)
	}
}
