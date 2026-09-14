package app

import (
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewGapMetadataCategoryRequiresTaskActivationEvidence(t *testing.T) {
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
	active := true
	inactive := false

	tests := []struct {
		name       string
		active     *bool
		activationErr error
		wantStatus string
		wantReason string
		want       string
	}{
		{name: "foreign repository", active: &inactive, wantStatus: reviewGapKnown, want: state.FixCategoryDocumentation},
		{name: "activated repository harness", active: &active, wantStatus: reviewGapKnown, want: state.FixCategoryMetadata},
		{name: "missing historical evidence", wantStatus: reviewGapUnknown, wantReason: reviewGapReasonRepositoryHarnessActivationMissing},
		{name: "unreadable historical evidence", activationErr: errors.New("broken evidence"), wantStatus: reviewGapUnknown, wantReason: reviewGapReasonRepositoryHarnessActivationUnreadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fix := reviewGapFix{}
			reviewGapFillCategories(&fix, previous, round, tt.active, tt.activationErr)
			if fix.CategoryStatus != tt.wantStatus {
				t.Fatalf("category status = %q want %q", fix.CategoryStatus, tt.wantStatus)
			}
			if fix.CategoryReason != tt.wantReason {
				t.Fatalf("category reason = %q want %q", fix.CategoryReason, tt.wantReason)
			}
			if tt.want == "" {
				if len(fix.Categories) != 0 {
					t.Fatalf("unknown categories = %#v", fix.Categories)
				}
				return
			}
			if len(fix.Categories) != 1 || fix.Categories[0] != tt.want {
				t.Fatalf("categories = %#v want %q", fix.Categories, tt.want)
			}
		})
	}
}
