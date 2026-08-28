package packet

import (
	"strings"
	"testing"
)

func TestReviewerSchemaAllowsNeedsSolDecision(t *testing.T) {
	schema, err := ReviewerSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		string(StatusNeedsSolDecision),
		`"decision"`, `"evidence"`, `"options"`, `"recommendation"`, `"test_obligations"`,
	} {
		if !strings.Contains(schema, value) {
			t.Fatalf("reviewer schema missing %s: %s", value, schema)
		}
	}

	riskFloor, err := RiskFloorReviewerSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(riskFloor, string(StatusNeedsSolDecision)) {
		t.Fatalf("risk-floor schema must stay NEEDS_SOL_REVIEW-only: %s", riskFloor)
	}
}

func TestValidateReviewerNeedsSolDecision(t *testing.T) {
	result := Result{
		Status:          StatusNeedsSolDecision,
		Risk:            RiskHigh,
		Decision:        "public-surface semantics are unresolved",
		Evidence:        "actual diff adds externally visible fields",
		Options:         "keep internal-only or expose fields",
		Recommendation:  "ask Sol before accepting the semantic choice",
		TestObligations: "verify selected public contract",
		Targets:         []string{"internal/timeline/timeline.go"},
	}
	if err := ValidateReviewerResult(result); err != nil {
		t.Fatalf("valid reviewer decision rejected: %v", err)
	}
	result.Risk = RiskLow
	if err := ValidateReviewerResult(result); err == nil {
		t.Fatal("LOW reviewer NEEDS_SOL_DECISION must be rejected")
	}
}
