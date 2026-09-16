package parentaction

import "encoding/json"

type PreparedSlot struct {
	Name         string `json:"name"`
	Placeholder  string `json:"placeholder"`
	RequiredWhen string `json:"required_when,omitempty"`
}

type preparedAlias Prepared

func (prepared Prepared) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		preparedAlias
		Slots       []PreparedSlot `json:"slots"`
		NextCommand []string       `json:"next_command"`
	}{
		preparedAlias: preparedAlias(prepared),
		Slots:         preparedSlots(prepared.Action),
		NextCommand:   []string{"glm-parent-action", prepared.Action, prepared.Token},
	})
}

func preparedSlots(action string) []PreparedSlot {
	switch Action(action) {
	case ActionDecision:
		return []PreparedSlot{
			{Name: "decision", Placeholder: decisionPlaceholder},
			{Name: "execution_unit", Placeholder: executionUnitPlaceholder},
			{Name: "milestones_json", Placeholder: `{"milestones":[]}`, RequiredWhen: "execution_unit=milestones"},
		}
	case ActionFix, ActionStartMilestones, ActionReviseMilestones:
		return []PreparedSlot{{Name: "payload", Placeholder: "__GLM_PARENT_ACTION_PAYLOAD__"}}
	default:
		return []PreparedSlot{}
	}
}
