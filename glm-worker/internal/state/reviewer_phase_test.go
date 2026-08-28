package state

import "testing"

func TestParseReviewerPhase(t *testing.T) {
	tests := []struct {
		phase    string
		number   int
		category string
		ok       bool
	}{
		{phase: "reviewer-1", number: 1, category: ReviewerPhaseCategoryReview, ok: true},
		{phase: "reviewer-12-result-correct", number: 12, category: ReviewerPhaseCategoryReview, ok: true},
		{phase: "reviewer-2-risk-floor", number: 2, category: ReviewerPhaseCategoryRiskFloor, ok: true},
		{phase: "reviewer-2-risk-floor-result-correct", number: 2, category: ReviewerPhaseCategoryRiskFloor, ok: true},
		{phase: "reviewer-3-high-floor", number: 3, category: ReviewerPhaseCategoryHighFloor, ok: true},
		{phase: "reviewer-3-high-floor-result-correct", number: 3, category: ReviewerPhaseCategoryHighFloor, ok: true},
		{phase: "worker-new"},
		{phase: "reviewer-report-only-1"},
		{phase: "reviewer-"},
		{phase: "reviewer-x"},
		{phase: "reviewer-1-unexpected"},
		{phase: "reviewer-1-high-floor-extra"},
	}
	for _, tc := range tests {
		t.Run(tc.phase, func(t *testing.T) {
			got, ok := ParseReviewerPhase(tc.phase)
			if ok != tc.ok {
				t.Fatalf("ParseReviewerPhase(%q) ok = %v want %v", tc.phase, ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.ReviewNumber != tc.number || got.Category != tc.category {
				t.Fatalf("ParseReviewerPhase(%q) = %#v want number=%d category=%q", tc.phase, got, tc.number, tc.category)
			}
		})
	}
}
