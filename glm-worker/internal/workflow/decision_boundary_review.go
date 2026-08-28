package workflow

import (
	"fmt"
	"strings"
)

const solDecisionBoundaryReviewMarker = "SOL_DECISION_BOUNDARY_REVIEW:"

func decisionBoundaryReviewContextBlock(activeTaskPath string, authority semanticDecisionAuthority) string {
	unresolved := authority.unresolved()
	if activeTaskPath == "" || len(unresolved) == 0 {
		return ""
	}

	names := make([]string, 0, len(unresolved))
	for _, axis := range unresolved {
		names = append(names, string(axis))
	}

	return fmt.Sprintf(`

%s
AUTHORITY_SOURCE: %s / ## %s
UNRESOLVED_AXES: %s
REVIEW_RULES:
- actual git diffを最初に確認し、worker reportや変更path名だけで意味選択を推測しない。
- actual diffがUNRESOLVED axisの意味を選択・確定している場合だけNEEDS_SOL_DECISIONを返す。
- UNRESOLVED axisを選択していない変更ではNEEDS_SOL_DECISIONを返さず、通常のPASS/FIX_REQUIRED/NEEDS_SOL_REVIEW判定を続ける。
- type/package/interface追加やproduction editであること自体をdecision-boundary違反とみなさない。
`, solDecisionBoundaryReviewMarker, activeTaskPath, solDecisionAuthorityHeading, strings.Join(names, ","))
}

func (w *Workflow) reviewerDecisionBoundaryContext(activeTaskPath string) (string, error) {
	if activeTaskPath == "" {
		return "", nil
	}
	authority, err := loadSemanticDecisionAuthority(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return "", err
	}
	return decisionBoundaryReviewContextBlock(activeTaskPath, authority), nil
}
