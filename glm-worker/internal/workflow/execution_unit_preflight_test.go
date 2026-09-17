package workflow

import (
	"strings"
	"testing"
)

func TestValidateDecisionExecutionUnitPayloadUsesCurrentMilestoneState(t *testing.T) {
	w, st, _, _ := newExecutionMilestoneWorkflow(t, []runnerStep{{structured: needsSolDecisionPacket()}})
	if err := w.ExecuteNewTask("implement the ACTIVE task"); err != nil {
		t.Fatal(err)
	}

	emptyMilestones := "EXECUTION_UNIT: milestones\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\ncontinue\n"
	if err := ValidateDecisionExecutionUnitPayload(w.config, st, emptyMilestones); err == nil || !strings.Contains(err.Error(), "existing pending milestone plan") {
		t.Fatalf("milestone disposition without definitions or plan was admitted: %v", err)
	}

	single := "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\ncontinue\n"
	if err := ValidateDecisionExecutionUnitPayload(w.config, st, single); err != nil {
		t.Fatalf("single disposition without a pending plan was rejected: %v", err)
	}

	definitions := []ExecutionMilestoneDefinition{
		{ID: "first", Scope: "first scope", Acceptance: "first accepted"},
		{ID: "second", Scope: "second scope", Acceptance: "second accepted"},
	}
	if err := w.initializeExecutionMilestones(definitions, st.ReadOr(activeTaskStateKey, "")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDecisionExecutionUnitPayload(w.config, st, single); err == nil || !strings.Contains(err.Error(), "cannot bypass pending execution milestones") {
		t.Fatalf("single disposition bypassed a pending milestone plan: %v", err)
	}
}
