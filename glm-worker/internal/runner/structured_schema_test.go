package runner

import (
	"encoding/json"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStructuredSchemaUsesRiskFloorContractOnlyForRiskFloorPhase(t *testing.T) {
	regular, err := structuredSchema(state.ReviewerRole, "reviewer-1")
	if err != nil {
		t.Fatal(err)
	}
	riskFloor, err := structuredSchema(state.ReviewerRole, "reviewer-1-risk-floor")
	if err != nil {
		t.Fatal(err)
	}

	regularRequired := schemaRequiredForTest(t, regular)
	if regularRequired["test_evidence"] || regularRequired["sol_question"] {
		t.Fatalf("regular reviewer required = %v", regularRequired)
	}
	riskFloorRequired := schemaRequiredForTest(t, riskFloor)
	for _, field := range []string{"test_evidence", "sol_question", "targets", "artifacts"} {
		if !riskFloorRequired[field] {
			t.Fatalf("risk-floor requiredに%sがありません: %v", field, riskFloorRequired)
		}
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(riskFloor), &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	status := properties["status"].(map[string]any)["enum"].([]any)
	risk := properties["risk"].(map[string]any)["enum"].([]any)
	if len(status) != 1 || status[0] != "NEEDS_SOL_REVIEW" {
		t.Fatalf("status enum = %v", status)
	}
	if len(risk) != 1 || risk[0] != "HIGH" {
		t.Fatalf("risk enum = %v", risk)
	}
}

func schemaRequiredForTest(t *testing.T, encoded string) map[string]bool {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(encoded), &schema); err != nil {
		t.Fatal(err)
	}
	result := map[string]bool{}
	for _, raw := range schema["required"].([]any) {
		result[raw.(string)] = true
	}
	return result
}

func TestStructuredSchemaHighFloorExcludesPass(t *testing.T) {
	for _, phase := range []string{"reviewer-1-high-floor", "reviewer-1-high-floor-result-correct"} {
		encoded, err := structuredSchema(state.ReviewerRole, phase)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(encoded), &schema); err != nil {
			t.Fatal(err)
		}
		properties := schema["properties"].(map[string]any)
		status := properties["status"].(map[string]any)["enum"].([]any)
		got := map[string]bool{}
		for _, value := range status {
			got[value.(string)] = true
		}
		for _, want := range []string{"FIX_REQUIRED", "NEEDS_SOL_REVIEW", "NEEDS_SOL_DECISION"} {
			if !got[want] {
				t.Fatalf("phase %s status enum = %v", phase, status)
			}
		}
		if got["PASS"] {
			t.Fatalf("phase %s unexpectedly permits PASS: %v", phase, status)
		}
	}
}
