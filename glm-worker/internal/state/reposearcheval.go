package state

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type RepoSearchMeasure struct {
	Calls             int            `json:"calls"`
	QueriesByCategory map[string]int `json:"queries_by_category,omitempty"`
	Outcomes          map[string]int `json:"outcomes,omitempty"`
	Hits              int            `json:"hits"`
	Misses            int            `json:"misses"`
	Fallbacks         int            `json:"fallbacks"`
	Skips             int            `json:"skips"`
	Other             int            `json:"other,omitempty"`
	Results           int            `json:"results"`
	DurationMS        int64          `json:"duration_ms"`
}

type RepoSearchTaskSummary struct {
	TaskID                string                  `json:"task_id"`
	Measure               RepoSearchMeasure       `json:"measure"`
	EventStatsConsistency string                  `json:"event_stats_consistency"`
	Review                TestImpactReviewSummary `json:"review"`
}

type RepoSearchSources struct {
	EventLog   string `json:"event_log"`
	TaskStats  string `json:"task_stats"`
	Categories string `json:"query_categories"`
	Outcomes   string `json:"outcome_classes"`
	Scope      string `json:"scope"`
}

type RepoSearchEvaluation struct {
	CodexReductionDelta string   `json:"codex_reduction_delta"`
	QualityDelta        string   `json:"quality_delta"`
	Reasons             []string `json:"reasons"`
}

type RepoSearchReport struct {
	Sources    RepoSearchSources       `json:"sources"`
	Retention  int                     `json:"retention"`
	Tasks      []RepoSearchTaskSummary `json:"tasks"`
	Totals     RepoSearchMeasure       `json:"totals"`
	Evaluation RepoSearchEvaluation    `json:"evaluation"`
}

const (
	RepoSearchCategoryWorkerNavigation    = "worker-repo-search"
	RepoSearchCategoryReviewerIndependent = "reviewer-repo-search"
)

const (
	RepoSearchOutcomeKnownTargetSkip          = "known-target-skip"
	RepoSearchOutcomeSearchHit                = "search-hit"
	RepoSearchOutcomeSearchEmptyFallback      = "search-empty-fallback"
	RepoSearchOutcomeSearchErrorFallback      = "search-error-fallback"
	RepoSearchOutcomeDiffSufficient           = "diff-sufficient"
	RepoSearchOutcomeIndependentDisabled      = "independent-search-disabled"
	RepoSearchOutcomeIndependentHit           = "independent-search-hit"
	RepoSearchOutcomeIndependentEmpty         = "independent-search-empty"
	RepoSearchOutcomeIndependentErrorFallback = "independent-search-error-fallback"
	RepoSearchOutcomeDiffSurfaceErrorFallback = "diff-surface-error-fallback"
)

const (
	RepoSearchOutcomeClassHit      = "hit"
	RepoSearchOutcomeClassMiss     = "miss"
	RepoSearchOutcomeClassFallback = "fallback"
	RepoSearchOutcomeClassSkip     = "skip"
	RepoSearchOutcomeClassOther    = "other"
)

const RepoSearchEventKind = "navigation"

const (
	RepoSearchConsistencyOk           = "ok"
	RepoSearchConsistencyMismatch     = "mismatch"
	RepoSearchConsistencyStatsMissing = "stats-missing"
	RepoSearchConsistencyUnverified   = "unverified"
)

const RepoSearchDeltaUnknown = "unknown"

const repoSearchEventLogSource = "task event logs (events/<task-id>.jsonl) carry one navigation-kind record per worker or reviewer BM25 route with the route phase as query category, the subtype as outcome, the search_paths length as result count, and duration_ms as the search wall duration; new route writes record no raw query text, candidate paths are the only hit evidence, and this report reads only counts and never echoes recorded strings"

const repoSearchTaskStatsSource = "task stats (current task-stats.json plus archived stats/<task-id>.json) keep additive per-task totals where repo_search_calls equals both the sum of repo_search_queries_by_category and the sum of repo_search_outcomes, and repo_search_results and repo_search_duration_ms accumulate one increment per recorded route outcome; archived stats written before this telemetry carry no repo-search counters, count as stats-missing, and are measured from retained events instead of being flagged as additive mismatches"

const repoSearchCategorySource = "query categories are the two deterministic call sites behind the GLM_WORKER_REPO_SEARCH toggle, worker-repo-search and reviewer-repo-search; no query-content classification is inferred"

const repoSearchOutcomeSource = "outcome classes derive at read time from the closed route-outcome set: hit covers search-hit and independent-search-hit, miss covers search-empty-fallback and independent-search-empty, fallback covers search-error-fallback, independent-search-error-fallback and diff-surface-error-fallback, skip covers known-target-skip, diff-sufficient and independent-search-disabled, and unknown values fall to other"

const repoSearchScopeSource = "the always-on exhaustive full-corpus proof scans stay outside this telemetry because they are not part of the search toggle surface whose Codex Reduction is measured"

func RepoSearchOutcomeClass(outcome string) string {
	switch outcome {
	case RepoSearchOutcomeSearchHit, RepoSearchOutcomeIndependentHit:
		return RepoSearchOutcomeClassHit
	case RepoSearchOutcomeSearchEmptyFallback, RepoSearchOutcomeIndependentEmpty:
		return RepoSearchOutcomeClassMiss
	case RepoSearchOutcomeSearchErrorFallback, RepoSearchOutcomeIndependentErrorFallback, RepoSearchOutcomeDiffSurfaceErrorFallback:
		return RepoSearchOutcomeClassFallback
	case RepoSearchOutcomeKnownTargetSkip, RepoSearchOutcomeDiffSufficient, RepoSearchOutcomeIndependentDisabled:
		return RepoSearchOutcomeClassSkip
	default:
		return RepoSearchOutcomeClassOther
	}
}

func RepoSearchMeasureFromStats(stats TaskStats) RepoSearchMeasure {
	measure := RepoSearchMeasure{
		Calls:      stats.RepoSearchCalls,
		Results:    stats.RepoSearchResults,
		DurationMS: stats.RepoSearchDurationMS,
	}
	for category, count := range stats.RepoSearchQueriesByCategory {
		addInt(&measure.QueriesByCategory, category, count)
	}
	for outcome, count := range stats.RepoSearchOutcomes {
		addInt(&measure.Outcomes, outcome, count)
		measure.absorbOutcomeClass(RepoSearchOutcomeClass(outcome), count)
	}
	return measure
}

func RepoSearchMeasureFromEvents(records []TaskEventRecord) RepoSearchMeasure {
	var measure RepoSearchMeasure
	for _, record := range records {
		if !IsRepoSearchRouteEvent(record) {
			continue
		}
		measure.absorbRoute(record.Phase, record.Subtype, len(record.SearchPaths), record.DurationMS)
	}
	return measure
}

func IsRepoSearchRouteEvent(record TaskEventRecord) bool {
	if record.Kind != RepoSearchEventKind {
		return false
	}
	return record.Phase == RepoSearchCategoryWorkerNavigation || record.Phase == RepoSearchCategoryReviewerIndependent
}

func (m *RepoSearchMeasure) absorbRoute(category string, outcome string, resultCount int, durationMS int64) {
	m.Calls++
	addInt(&m.QueriesByCategory, category, 1)
	addInt(&m.Outcomes, outcome, 1)
	m.absorbOutcomeClass(RepoSearchOutcomeClass(outcome), 1)
	m.Results += resultCount
	m.DurationMS += durationMS
}

func (m *RepoSearchMeasure) absorbOutcomeClass(class string, count int) {
	switch class {
	case RepoSearchOutcomeClassHit:
		m.Hits += count
	case RepoSearchOutcomeClassMiss:
		m.Misses += count
	case RepoSearchOutcomeClassFallback:
		m.Fallbacks += count
	case RepoSearchOutcomeClassSkip:
		m.Skips += count
	default:
		m.Other += count
	}
}

func repoSearchMeasuresEqual(a RepoSearchMeasure, b RepoSearchMeasure) bool {
	return a.Calls == b.Calls && a.Results == b.Results && a.DurationMS == b.DurationMS &&
		maps.Equal(a.QueriesByCategory, b.QueriesByCategory) && maps.Equal(a.Outcomes, b.Outcomes)
}

func BuildRepoSearchReport(events []TaskEvents, statsByTask map[string]TaskStats, reviews map[string]TestImpactReviewSummary) RepoSearchReport {
	report := RepoSearchReport{
		Sources: RepoSearchSources{
			EventLog:   repoSearchEventLogSource,
			TaskStats:  repoSearchTaskStatsSource,
			Categories: repoSearchCategorySource,
			Outcomes:   repoSearchOutcomeSource,
			Scope:      repoSearchScopeSource,
		},
		Retention: retainedTaskEventLogs,
		Tasks:     []RepoSearchTaskSummary{},
		Evaluation: RepoSearchEvaluation{
			CodexReductionDelta: RepoSearchDeltaUnknown,
			QualityDelta:        RepoSearchDeltaUnknown,
			Reasons:             []string{},
		},
	}
	eventsByTask := make(map[string][]TaskEventRecord, len(events))
	for _, task := range events {
		if len(task.Records) == 0 {
			continue
		}
		eventsByTask[task.TaskID] = task.Records
	}
	for _, taskID := range repoSearchReportTaskIDs(eventsByTask, statsByTask) {
		summary := RepoSearchTaskSummary{TaskID: taskID, Review: reviews[taskID]}
		summary.Review = normalizeRepoSearchReview(summary.Review)
		stats, hasStats := statsByTask[taskID]
		records, hasEvents := eventsByTask[taskID]
		switch {
		case !hasStats || !repoSearchStatsHaveRecordedRoutes(stats):
			summary.EventStatsConsistency = RepoSearchConsistencyStatsMissing
			summary.Measure = RepoSearchMeasureFromEvents(records)
		case !hasEvents:
			summary.EventStatsConsistency = RepoSearchConsistencyUnverified
			summary.Measure = RepoSearchMeasureFromStats(stats)
		default:
			summary.Measure = RepoSearchMeasureFromStats(stats)
			if repoSearchMeasuresEqual(summary.Measure, RepoSearchMeasureFromEvents(records)) {
				summary.EventStatsConsistency = RepoSearchConsistencyOk
			} else {
				summary.EventStatsConsistency = RepoSearchConsistencyMismatch
			}
		}
		report.Tasks = append(report.Tasks, summary)
		absorbRepoSearchMeasure(&report.Totals, summary.Measure)
	}
	report.Evaluation.Reasons = repoSearchReasons(report)
	return report
}

func repoSearchStatsHaveRecordedRoutes(stats TaskStats) bool {
	return stats.RepoSearchCalls > 0 || stats.RepoSearchResults > 0 || stats.RepoSearchDurationMS > 0 ||
		len(stats.RepoSearchQueriesByCategory) > 0 || len(stats.RepoSearchOutcomes) > 0
}

func normalizeRepoSearchReview(review TestImpactReviewSummary) TestImpactReviewSummary {
	if review.Outcome == "" {
		review.Outcome = TestImpactReviewOutcomeUnknown
	}
	return review
}

func repoSearchReportTaskIDs(eventsByTask map[string][]TaskEventRecord, statsByTask map[string]TaskStats) []string {
	ids := make(map[string]bool)
	for taskID, records := range eventsByTask {
		for _, record := range records {
			if IsRepoSearchRouteEvent(record) {
				ids[taskID] = true
				break
			}
		}
	}
	for taskID, stats := range statsByTask {
		if stats.RepoSearchCalls > 0 {
			ids[taskID] = true
		}
	}
	taskIDs := make([]string, 0, len(ids))
	for taskID := range ids {
		taskIDs = append(taskIDs, taskID)
	}
	slices.Sort(taskIDs)
	return taskIDs
}

func absorbRepoSearchMeasure(totals *RepoSearchMeasure, measure RepoSearchMeasure) {
	totals.Calls += measure.Calls
	totals.Hits += measure.Hits
	totals.Misses += measure.Misses
	totals.Fallbacks += measure.Fallbacks
	totals.Skips += measure.Skips
	totals.Other += measure.Other
	totals.Results += measure.Results
	totals.DurationMS += measure.DurationMS
	for category, count := range measure.QueriesByCategory {
		addInt(&totals.QueriesByCategory, category, count)
	}
	for outcome, count := range measure.Outcomes {
		addInt(&totals.Outcomes, outcome, count)
	}
}

func repoSearchReasons(report RepoSearchReport) []string {
	reasons := []string{"direct-mode Codex runs delegate no glm-worker repo-search routes, so this telemetry is orchestrated-side only and Codex Reduction stays unknown until the permission-gated live A/B compares glm_usage"}
	if report.Totals.Calls == 0 {
		reasons = append(reasons, "no repo-search route outcomes are recorded in durable task stats")
	}
	reasons = append(reasons, fmt.Sprintf(
		"event retention keeps the most recent %d past task event logs plus the current task, so per-route cross-checks cover only retained tasks; totals sum each task's chosen measure, which uses recorded task stats when repo-search counters exist and retained events for tasks whose stats carry none",
		report.Retention))
	if summary := repoSearchConsistencySummary(report.Tasks); summary != "" {
		reasons = append(reasons, summary)
	}
	reasons = append(reasons, "the repo-search feature is default-on, so retained telemetry holds no disabled arm to contrast task quality against, and a controlled quality contrast requires the permission-gated A/B schema")
	reasons = append(reasons, "hit, miss, fallback and skip counts are derived at read time from the recorded outcome map, so the additive outcome counters remain the single recorded source")
	return reasons
}

func repoSearchConsistencySummary(tasks []RepoSearchTaskSummary) string {
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.EventStatsConsistency]++
	}
	if counts[RepoSearchConsistencyMismatch] == 0 && counts[RepoSearchConsistencyStatsMissing] == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	slices.Sort(statuses)
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s %d", status, counts[status]))
	}
	return fmt.Sprintf("event-vs-stats additive consistency flags %s across reported tasks", strings.Join(parts, ", "))
}
