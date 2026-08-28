package state

import (
	"strconv"
	"strings"
)

type ReviewerPhaseInfo struct {
	ReviewNumber int
	Category     string
}

const (
	ReviewerPhaseCategoryReview    = "reviewer"
	ReviewerPhaseCategoryRiskFloor = "reviewer-risk-floor"
	ReviewerPhaseCategoryHighFloor = "reviewer-high-floor"
)

func ParseReviewerPhase(phase string) (ReviewerPhaseInfo, bool) {
	rest, ok := strings.CutPrefix(phase, ReviewerPhaseCategoryReview+"-")
	if !ok {
		return ReviewerPhaseInfo{}, false
	}
	numberText, suffix, separated := strings.Cut(rest, "-")
	if numberText == "" || !reviewerPhaseDigits(numberText) {
		return ReviewerPhaseInfo{}, false
	}
	reviewNumber, err := strconv.Atoi(numberText)
	if err != nil {
		return ReviewerPhaseInfo{}, false
	}
	category := ReviewerPhaseCategoryReview
	if separated {
		switch suffix {
		case "result-correct":
		case "risk-floor", "risk-floor-result-correct":
			category = ReviewerPhaseCategoryRiskFloor
		case "high-floor", "high-floor-result-correct":
			category = ReviewerPhaseCategoryHighFloor
		default:
			return ReviewerPhaseInfo{}, false
		}
	}
	return ReviewerPhaseInfo{ReviewNumber: reviewNumber, Category: category}, true
}

func ReviewerPhaseCategory(phase string) string {
	parsed, ok := ParseReviewerPhase(phase)
	if !ok {
		return phase
	}
	return parsed.Category
}

func reviewerPhaseDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
