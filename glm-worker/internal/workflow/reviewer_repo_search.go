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

func (w *Workflow) reviewerDiffFirstNavigation(request string, reviewNumber int) (string, error) {
	parentMetadataFilterActive, err := RepositoryHarnessActive(w.config.RepoRoot, w.state)
	if err != nil {
		return "", err
	}
	return w.reviewerDiffFirstNavigationWithHarness(request, reviewNumber, parentMetadataFilterActive)
}

func (w *Workflow) reviewerDiffFirstNavigationWithHarness(request string, reviewNumber int, parentMetadataFilterActive bool) (string, error) {
	collector := w.collectChangedPaths
	if collector == nil {
		collector = collectChangedPaths
	}
	baseline := w.state.ReadOr("baseline-head", "")
	paths, err := collector(w.config.RepoRoot, baseline)
	if err != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffErrorFallback, nil, 0)
		return renderReviewerDiffFirstNavigation(nil, reviewerSearchDiffErrorFallback, "", nil), nil
	}
	paths = uniqueSortedPaths(paths)
	impactPaths := reviewerImpactPaths(paths, parentMetadataFilterActive)
	if len(impactPaths) == 0 {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchDiffSufficient, nil, 0)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchDiffSufficient, "", nil), nil
	}
	if !w.config.RepoSearch {
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchDisabled, "", nil), nil
	}

	impactTerms := collectReviewerDiffImpactTerms(w.config.RepoRoot, baseline, reviewerDiffImpactPaths(paths, parentMetadataFilterActive))
	query := reviewerIndependentSearchQuery(request, impactPaths, impactTerms)
	timer := w.newRepoSearchTimer()
	report, searchErr := timer.run(context.Background(), w.config.RepoRoot, query, reposearch.Options{MaxResults: RepoSearchMaxResults})
	if searchErr != nil {
		w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, reviewerSearchErrorFallback, nil, timer.elapsed)
		return renderReviewerDiffFirstNavigation(paths, reviewerSearchErrorFallback, query, nil), nil
	}
	candidates := excludeChangedPaths(report.Results, paths)
	outcome := reviewerSearchHit
	if len(candidates) == 0 {
		outcome = reviewerSearchEmpty
	}
	w.recordRepoSearchOutcome(reviewerRepoSearchPhase, state.ReviewerRole, reviewNumber+1, outcome, candidates, timer.elapsed)
	return renderReviewerDiffFirstNavigation(paths, outcome, query, candidates), nil
}

func reviewerImpactPaths(paths []string, parentMetadataFilterActive bool) []string {
	impact := make([]string, 0, len(paths))
	for _, path := range paths {
		if parentMetadataFilterActive && state.IsParentManagedPath(path) {
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

func reviewerDiffImpactPaths(paths []string, parentMetadataFilterActive bool) []string {
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if parentMetadataFilterActive && state.IsParentManagedPath(path) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
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
		for _, term := range strings.FieldsFunc(line[1:], func(r rune) bool {
			return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == '/')
		}) {
			term = strings.Trim(term, ".-/")
			if len(term) < 3 {
				continue
			}
			if _, ok := seen[term]; ok {
				continue
			}
			seen[term] = struct{}{}
			terms = append(terms, term)
			if len(terms) == limit {
				return terms
			}
		}
	}
	return terms
}

func isReviewerImpactDiffLine(line string) bool {
	return len(line) > 1 && (line[0] == '+' || line[0] == '-') && !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---")
}

func reviewerIndependentSearchQuery(request string, impactPaths, impactTerms []string) string {
	parts := make([]string, 0, len(impactPaths)+len(impactTerms)+1)
	request = strings.TrimSpace(request)
	if request != "" {
		parts = append(parts, request)
	}
	if len(impactPaths) > 0 {
		parts = append(parts, "review impact paths: "+strings.Join(impactPaths, " "))
	}
	if len(impactTerms) > 0 {
		parts = append(parts, "review impact terms: "+strings.Join(impactTerms, " "))
	}
	return strings.Join(parts, "\n")
}

func excludeChangedPaths(results []reposearch.Result, changedPaths []string) []reposearch.Result {
	changed := make(map[string]struct{}, len(changedPaths))
	for _, path := range changedPaths {
		changed[path] = struct{}{}
	}
	candidates := make([]reposearch.Result, 0, len(results))
	for _, result := range results {
		if _, ok := changed[result.Path]; ok {
			continue
		}
		candidates = append(candidates, result)
	}
	return candidates
}

func uniqueSortedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	sort.Strings(unique)
	return unique
}

func renderReviewerDiffFirstNavigation(paths []string, outcome, query string, candidates []reposearch.Result) string {
	var b strings.Builder
	b.WriteString("REVIEW_DIFF_FIRST_NAVIGATION:\n")
	b.WriteString("AUTHORITY: wrapper-captured-current-task-diff\n")
	for _, path := range paths {
		b.WriteString("CHANGED_PATH: ")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	performed := outcome == reviewerSearchHit || outcome == reviewerSearchEmpty || outcome == reviewerSearchErrorFallback
	if performed {
		b.WriteString("INDEPENDENT_SEARCH: performed\n")
		b.WriteString("INDEPENDENT_QUERY: ")
		b.WriteString(query)
		b.WriteByte('\n')
	} else {
		b.WriteString("INDEPENDENT_SEARCH: skipped\n")
	}
	b.WriteString("SEARCH_OUTCOME: ")
	b.WriteString(outcome)
	b.WriteByte('\n')
	for _, candidate := range candidates {
		b.WriteString(fmt.Sprintf("IMPACT_CANDIDATE: %s:%d\n", candidate.Path, candidate.Line))
	}
	b.WriteString("WORKER_SEARCH_AUTHORITY: none\n")
	b.WriteString("END_REVIEW_DIFF_FIRST_NAVIGATION")
	return b.String()
}
