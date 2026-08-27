package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	reviewerRepoSearchPhase         = "reviewer-repo-search"
	reviewerSearchDiffSufficient    = "diff-sufficient"
	reviewerSearchHit               = "independent-search-hit"
	reviewerSearchEmpty             = "independent-search-empty"
	reviewerSearchErrorFallback     = "independent-search-error-fallback"
	reviewerSearchDiffErrorFallback = "diff-surface-error-fallback"
)

func (w *Workflow) reviewerDiffFirstContext(request string, reviewNumber int) string {
	collector := w.collectChangedPaths
	if collector == nil {
		collector = collectChangedPaths
	}
	paths, err := collector(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
	if err != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffErrorFallback, "", nil)
		return renderReviewerDiffFirstNavigation(nil, reviewerSearchDiffErrorFallback, "", nil)
	}
	paths = uniqueSortedPaths(paths)
	impactPaths := reviewerImpactPaths(paths)
	if len(impactPaths) == 0 {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffSufficient, "", nil)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchDiffSufficient, "", nil)
	}

	query := reviewerIndependentSearchQuery(request, impactPaths)
	search := w.repoSearch
	if search == nil {
		search = reposearch.Search
	}
	report, searchErr := search(context.Background(), w.config.RepoRoot, query, reposearch.Options{MaxResults: repoSearchMaxResults})
	if searchErr != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchErrorFallback, query, nil)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchErrorFallback, query, nil)
	}
	candidates := excludeChangedPaths(report.Results, paths)
	outcome := reviewerSearchHit
	if len(candidates) == 0 {
		outcome = reviewerSearchEmpty
	}
	w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, outcome, query, candidates)
	return renderReviewerDiffFirstNavigation(paths, outcome, query, candidates)
}

func reviewerImpactPaths(paths []string) []string {
	impact := make([]string, 0, len(paths))
	for _, path := range paths {
		if isParentManagedReviewPath(path) {
			continue
		}
		critical, category := IsCriticalPath(path)
		if critical {
			impact = append(impact, path)
			continue
		}
		switch category {
		case testPathCategory, testFixturePathCategory, testHarnessPathCategory, "docs", "repo-metadata":
			continue
		default:
			impact = append(impact, path)
		}
	}
	return impact
}

func isParentManagedReviewPath(path string) bool {
	return path == state.ParentRulesFile || path == state.ParentPlanFile || path == state.ParentHistoryFile || strings.HasPrefix(path, state.ParentTasksDir+"/")
}

func reviewerIndependentSearchQuery(request string, paths []string) string {
	return strings.TrimSpace(request) + "\nreview impact paths: " + strings.Join(paths, " ")
}

func excludeChangedPaths(results []reposearch.Result, changed []string) []reposearch.Result {
	changedSet := make(map[string]struct{}, len(changed))
	for _, path := range changed {
		changedSet[path] = struct{}{}
	}
	filtered := make([]reposearch.Result, 0, len(results))
	for _, result := range results {
		if _, found := changedSet[result.Path]; found {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func uniqueSortedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, found := seen[path]; found {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	sort.Strings(unique)
	return unique
}

func renderReviewerDiffFirstNavigation(paths []string, outcome string, query string, candidates []reposearch.Result) string {
	var block strings.Builder
	block.WriteString("REVIEW_DIFF_FIRST_NAVIGATION:\n")
	block.WriteString(fmt.Sprintf("CHANGED_PATH_COUNT: %d\n", len(paths)))
	for _, path := range paths {
		block.WriteString("CHANGED_PATH: ")
		block.WriteString(path)
		block.WriteByte('\n')
	}
	searchMode := "skipped"
	if outcome != reviewerSearchDiffSufficient && outcome != reviewerSearchDiffErrorFallback {
		searchMode = "performed"
	}
	block.WriteString("INDEPENDENT_SEARCH: ")
	block.WriteString(searchMode)
	block.WriteByte('\n')
	block.WriteString("SEARCH_OUTCOME: ")
	block.WriteString(outcome)
	block.WriteByte('\n')
	if query != "" {
		block.WriteString("INDEPENDENT_QUERY: ")
		block.WriteString(strings.ReplaceAll(query, "\n", " "))
		block.WriteByte('\n')
	}
	for _, result := range candidates {
		block.WriteString("IMPACT_CANDIDATE: ")
		block.WriteString(result.Path)
		if result.Line > 0 {
			block.WriteString(fmt.Sprintf(":%d", result.Line))
		}
		block.WriteByte('\n')
	}
	block.WriteString("WORKER_SEARCH_AUTHORITY: none\n")
	block.WriteString("AUTHORITY: navigation-only\n")
	block.WriteString("END_REVIEW_DIFF_FIRST_NAVIGATION")
	return block.String()
}
