package app

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewGapMetadataCategoryRequiresRepositoryHarness(t *testing.T) {
	previous := &state.RoundRecord{Paths: []state.RoundPathState{{
		Path:       state.ParentPlanFile,
		Class:      "doc",
		FullDigest: "before",
	}}}
	round := state.RoundRecord{Paths: []state.RoundPathState{{
		Path:       state.ParentPlanFile,
		Class:      "doc",
		FullDigest: "after",
	}}}

	tests := []struct {
		name   string
		active bool
		want   string
	}{
		{name: "foreign repository", active: false, want: state.FixCategoryDocumentation},
		{name: "activated repository harness", active: true, want: state.FixCategoryMetadata},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := reviewGapFix{}
			reviewGapFillCategories(&fix, previous, round, tt.active)
			if fix.CategoryStatus != reviewGapKnown {
				t.Fatalf("category status = %q", fix.CategoryStatus)
			}
			if len(fix.Categories) != 1 || fix.Categories[0] != tt.want {
				t.Fatalf("categories = %#v want %q", fix.Categories, tt.want)
			}
		})
	}
}
