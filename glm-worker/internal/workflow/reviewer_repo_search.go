package workflow

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"unicode"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	reviewerRepoSearchPhase         = state.RepoSearchCategoryReviewerIndependent
	reviewerSearchDiffSufficient    = state.RepoSearchOutcomeDiffSufficient
	reviewerSearchDisabled          = state.RepoSearchOutcomeIndependentDisabled
	reviewerSearchHit               = state.RepoSearchOutcomeIndependentHit
	reviewerSearchEmpty             = state.RepoSearchOutcomeIndependentEmpty
	reviewerSearchErrorFallback     = state.RepoSearchOutcomeIndependentErrorFallback
	reviewerSearchDiffErrorFallback = state.RepoSearchOutcomeDiffSurfaceErrorFallback
	reviewerDiffImpactTermLimit     = 32
)

func (w *Workflow) reviewerDiffFirstContext(request string, reviewNumber int) string {
	collector := w.collectChangedPaths
	if collector == nil {
		collector = collectChangedPaths
	}
	baseline := w.state.ReadOr("baseline-head", "")
	paths, err := collector(w.config.RepoRoot, baseline)
	if err != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffErrorFallback, nil, 0)
		return renderReviewerDiffFirstNavigation(nil, reviewerSearchDiffErrorFallback, "", nil)
	}
	paths = uniqueSortedPaths(paths)
	impactPaths := reviewerImpactPaths(paths)
	if len(impactPaths) == 0 {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffSufficient, nil, 0)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchDiffSufficient, "", nil)
	}
	if !w.config.RepoSearch {
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchDisabled, "", nil)
	}

	impactTerms := collectReviewerDiffImpactTerms(w.config.RepoRoot, baseline, reviewerDiffImpactPaths(paths))
	query := reviewerIndependentSearchQuery(request, impactPaths, impactTerms)
	timer := w.newRepoSearchTimer()
	report, searchErr := timer.run(context.Background(), w.config.RepoRoot, query, reposearch.Options{MaxResults: RepoSearchMaxResults})
	if searchErr != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchErrorFallback, nil, timer.elapsed)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchErrorFallback, query, nil)
	}
	candidates := excludeChangedPaths(report.Results, paths)
	outcome := reviewerSearchHit
	if len(candidates) == 0 {
		outcome = reviewerSearchEmpty
	}
	w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, outcome, candidates, timer.elapsed)
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

func reviewerDiffImpactPaths(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if isParentManagedReviewPath(path) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func isParentManagedReviewPath(path string) bool {
	return state.IsParentManagedPath(path)
}

func collectReviewerDiffImpactTerms(repoRoot, baseline string, paths []string) []string {
	if strings.TrimSpace(baseline) == "" || len(paths) == 0 {
		return nil
	}
	args := []string{"-C", repoRoot, "diff", "--unified=0", "--no-ext-diff", baseline, "--"}
	args = append(args, paths...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	return extractReviewerDiffImpactTerms(string(output), reviewerDiffImpactTermLimit)
}

func extractReviewerDiffImpactTerms(diff string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]struct{}, limit)
	terms := make([]string, 0, limit)
	for _, line := range strings.Split(diff, "\n") {
		if !isReviewerImpactDiffLine(line) {
			continue
		}
		terms = appendReviewerImpactTerms(terms, seen, line[1:], limit)
		if len(terms) == limit {
			break
		}
	}
	return terms
}

func isReviewerImpactDiffLine(line string) bool {
	if len(line) < 2 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
		return false
	}
	return line[0] == '+' || line[0] == '-'
}

func appendReviewerImpactTerms(terms []string, seen map[string]struct{}, line string, limit int) []string {
	for _, term := range strings.FieldsFunc(line, reviewerImpactTermSeparator) {
		term = strings.Trim(term, "./-")
		if len(term) < 3 {
			continue
		}
		if _, found := seen[term]; found {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) == limit {
			break
		}
	}
	return terms
}

func reviewerImpactTermSeparator(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '.' && r != '/' && r != '-'
}

func reviewerIndependentSearchQuery(request string, paths []string, impactTerms []string) string {
	var query strings.Builder
	query.WriteString(strings.TrimSpace(request))
	query.WriteString("\nreview impact paths: ")
	query.WriteString(strings.Join(paths, " "))
	if len(impactTerms) > 0 {
		query.WriteString("\nreview diff impact terms: ")
		query.WriteString(strings.Join(impactTerms, " "))
	}
	return query.String()
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
	if outcome != reviewerSearchDiffSufficient && outcome != reviewerSearchDiffErrorFallback && outcome != reviewerSearchDisabled {
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
