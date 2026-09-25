package packet

import (
	"strings"
	"testing"
)

func TestNeedsSolReviewTargetStructureValidatedAtPacketBoundary(t *testing.T) {
	base := Result{
		Status:              StatusNeedsSolReview,
		Risk:                RiskHigh,
		Summary:             "review",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "tests passed",
		Issues:              "issue",
		ResidualRisk:        "risk",
		SolQuestion:         "question",
	}

	for _, target := range []string{
		"glm-worker/internal/packet/validate.go:validateTargets",
		"glm-worker/internal/packet/validate.go:10",
		"glm-worker/internal/packet/validate.go:10-20",
		"does/not/exist.go:MissingSymbol",
	} {
		t.Run("valid "+target, func(t *testing.T) {
			result := base
			result.Targets = []string{target}
			if err := ValidateReviewerResult(result); err != nil {
				t.Fatalf("structurally valid target %q rejected: %v", target, err)
			}
		})
	}

	liveMalformed := "IMPLEMENTATION_TASKS/system-one-dogfood-evidence-shadow-eval.md (Contract追記・終了時Go/No-Go要件)"
	result := base
	result.Targets = []string{liveMalformed}
	err := ValidateReviewerResult(result)
	if err == nil {
		t.Fatalf("malformed live target %q was accepted", liveMalformed)
	}
	if !IsConstraintError(err) {
		t.Fatalf("malformed target error must be a constraint error: %v", err)
	}
	if !strings.Contains(err.Error(), "repository相対path:locator形式") {
		t.Fatalf("malformed target error does not identify the structural contract: %v", err)
	}
	if category := RejectCategory(err); category != "targets-none" {
		t.Fatalf("reject category = %q, want targets-none", category)
	}
}

func TestNonSolReviewTargetSemanticsRemainUnchanged(t *testing.T) {
	result := Result{
		Status:              StatusPass,
		Risk:                RiskLow,
		Summary:             "review",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "tests passed",
		Issues:              "none",
		ResidualRisk:        "none",
		Targets:             []string{"conceptual target without locator"},
	}
	if err := ValidateReviewerResult(result); err != nil {
		t.Fatalf("non-NEEDS_SOL_REVIEW target semantics changed: %v", err)
	}
}
