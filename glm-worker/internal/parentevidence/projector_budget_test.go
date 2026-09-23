package parentevidence

import (
	"strings"
	"testing"
)

func TestApplyTotalBudgetRecomputesStatusWhenMetadataRemainsOversize(t *testing.T) {
	metadata := strings.Repeat("x", MaxOutputBytes)
	output := Output{
		Status: StatusOK,
		Parts: []Part{{
			Kind:   "diff",
			Detail: metadata,
			Status: PartProjected,
			Diff: &DiffBody{
				Question: metadata,
				Body:     "body that can be stripped",
			},
		}},
	}
	if outputSize(output) <= MaxOutputBytes {
		t.Fatal("fixture must exceed total output budget")
	}

	applyTotalBudget(&output)

	if output.Parts[0].Status != PartRefinement {
		t.Fatalf("part status = %q, want %q", output.Parts[0].Status, PartRefinement)
	}
	if output.Status != StatusRequired {
		t.Fatalf("output status = %q, want %q", output.Status, StatusRequired)
	}
}
