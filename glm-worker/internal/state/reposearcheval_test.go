package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func newRepoSearchEvalTestStore(t *testing.T) *StateStore {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("b", 64),
		StateBase: filepath.Join(root, "state"),
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestRepoSearchOutcomeClassCoversClosedOutcomeSet(t *testing.T) {
	tests := []struct {
		outcome string
		want    string
	}{
		{RepoSearchOutcomeSearchHit, RepoSearchOutcomeClassHit},
		{RepoSearchOutcomeIndependentHit, RepoSearchOutcomeClassHit},
		{RepoSearchOutcomeSearchEmptyFallback, RepoSearchOutcomeClassMiss},
		{RepoSearchOutcomeIndependentEmpty, RepoSearchOutcomeClassMiss},
		{RepoSearchOutcomeSearchErrorFallback, RepoSearchOutcomeClassFallback},
		{RepoSearchOutcomeIndependentErrorFallback, RepoSearchOutcomeClassFallback},
		{RepoSearchOutcomeDiffSurfaceErrorFallback, RepoSearchOutcomeClassFallback},
		{RepoSearchOutcomeKnownTargetSkip, RepoSearchOutcomeClassSkip},
		{RepoSearchOutcomeDiffSufficient, RepoSearchOutcomeClassSkip},
		{RepoSearchOutcomeIndependentDisabled, RepoSearchOutcomeClassSkip},
		{"unexpected-outcome", RepoSearchOutcomeClassOther},
	}
	for _, test := range tests {
		if got := RepoSearchOutcomeClass(test.outcome); got != test.want {
			t.Fatalf("RepoSearchOutcomeClass(%q) = %q want %q", test.outcome, got, test.want)
		}
	}
}

func TestRecordRepoSearchOutcomeKeepsAdditiveConsistency(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordRepoSearchOutcome(RepoSearchCategoryWorkerNavigation, RepoSearchOutcomeSearchHit, 3, 1500*time.Millisecond)
	st.RecordRepoSearchOutcome(RepoSearchCategoryWorkerNavigation, RepoSearchOutcomeSearchEmptyFallback, 0, 500*time.Millisecond)
	st.RecordRepoSearchOutcome(RepoSearchCategoryReviewerIndependent, RepoSearchOutcomeIndependentErrorFallback, 0, 900*time.Millisecond)
	st.RecordRepoSearchOutcome("", RepoSearchOutcomeSearchHit, 5, time.Second)
	st.RecordRepoSearchOutcome(RepoSearchCategoryWorkerNavigation, "", 5, time.Second)

	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.RepoSearchCalls != 3 {
		t.Fatalf("repo_search_calls = %d want 3", stats.RepoSearchCalls)
	}
	if sumIntMapForTest(stats.RepoSearchQueriesByCategory) != 3 || sumIntMapForTest(stats.RepoSearchOutcomes) != 3 {
		t.Fatalf("category/outcome総和がcallsと一致しません: %+v", stats)
	}
	if stats.RepoSearchQueriesByCategory[RepoSearchCategoryWorkerNavigation] != 2 ||
		stats.RepoSearchQueriesByCategory[RepoSearchCategoryReviewerIndependent] != 1 {
		t.Fatalf("queries_by_category = %+v", stats.RepoSearchQueriesByCategory)
	}
	if stats.RepoSearchResults != 3 || stats.RepoSearchDurationMS != 2900 {
		t.Fatalf("results=%d duration=%d want 3/2900", stats.RepoSearchResults, stats.RepoSearchDurationMS)
	}
	measure := RepoSearchMeasureFromStats(stats)
	if measure.Hits != 1 || measure.Misses != 1 || measure.Fallbacks != 1 || measure.Skips != 0 || measure.Other != 0 {
		t.Fatalf("class counts = %+v", measure)
	}
	if measure.Calls != sumIntMapForTest(measure.QueriesByCategory) || measure.Calls != sumIntMapForTest(measure.Outcomes) {
		t.Fatalf("measure加法整合が崩れています: %+v", measure)
	}
}

func TestRepoSearchMeasureFromEventsReadsOnlyRouteEvents(t *testing.T) {
	records := []TaskEventRecord{
		{Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeSearchHit,
			SearchPaths: []string{"a.go", "b.go"}, DurationMS: 1200},
		{Kind: RepoSearchEventKind, Phase: RepoSearchCategoryReviewerIndependent, Subtype: RepoSearchOutcomeIndependentEmpty,
			DurationMS: 300},
		{Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeKnownTargetSkip},
		{Kind: "exhaustive-search", Phase: "worker-exhaustive-search", Subtype: "full-corpus-proof",
			SearchPaths: []string{"c.go"}, DurationMS: 9000},
		{Kind: RepoSearchEventKind, Phase: "worker-new", Subtype: "other-navigation"},
	}
	measure := RepoSearchMeasureFromEvents(records)
	if measure.Calls != 3 || measure.Results != 2 || measure.DurationMS != 1500 {
		t.Fatalf("measure = %+v", measure)
	}
	if measure.Hits != 1 || measure.Misses != 1 || measure.Skips != 1 || measure.Fallbacks != 0 {
		t.Fatalf("class counts = %+v", measure)
	}
	if measure.QueriesByCategory[RepoSearchCategoryWorkerNavigation] != 2 ||
		measure.QueriesByCategory[RepoSearchCategoryReviewerIndependent] != 1 {
		t.Fatalf("queries_by_category = %+v", measure.QueriesByCategory)
	}
}

func repoSearchStatsFixture(taskID string) TaskStats {
	stats := TaskStats{Version: 3, TaskID: taskID, Status: TaskStatusComplete}
	stats.RepoSearchCalls = 2
	stats.RepoSearchQueriesByCategory = map[string]int{RepoSearchCategoryWorkerNavigation: 2}
	stats.RepoSearchOutcomes = map[string]int{RepoSearchOutcomeSearchHit: 1, RepoSearchOutcomeSearchErrorFallback: 1}
	stats.RepoSearchResults = 4
	stats.RepoSearchDurationMS = 800
	return stats
}

func repoSearchEventsFixture(taskID string) TaskEvents {
	return TaskEvents{TaskID: taskID, Records: []TaskEventRecord{
		{Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeSearchHit,
			SearchPaths: []string{"a.go", "b.go", "c.go", "d.go"}, DurationMS: 650},
		{Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeSearchErrorFallback,
			DurationMS: 150},
	}}
}

func TestBuildRepoSearchReportCrossChecksEventsAndStats(t *testing.T) {
	mismatchStats := repoSearchStatsFixture("mismatch-task")
	mismatchStats.RepoSearchResults = 9
	statsOnly := repoSearchStatsFixture("stats-only-task")
	legacyStats := TaskStats{Version: 3, TaskID: "legacy-task", Status: TaskStatusComplete}
	events := []TaskEvents{
		repoSearchEventsFixture("consistent-task"),
		repoSearchEventsFixture("mismatch-task"),
		repoSearchEventsFixture("events-only-task"),
		repoSearchEventsFixture("legacy-task"),
	}
	statsByTask := map[string]TaskStats{
		"consistent-task": repoSearchStatsFixture("consistent-task"),
		"mismatch-task":   mismatchStats,
		"stats-only-task": statsOnly,
		"legacy-task":     legacyStats,
	}
	reviews := map[string]TestImpactReviewSummary{
		"consistent-task": {Outcome: ModelRoutingQualityReviewPass, PassCalls: 1},
	}

	report := BuildRepoSearchReport(events, statsByTask, reviews)

	if report.Retention != retainedTaskEventLogs {
		t.Fatalf("retention = %d", report.Retention)
	}
	consistency := map[string]string{}
	for _, task := range report.Tasks {
		consistency[task.TaskID] = task.EventStatsConsistency
	}
	if consistency["consistent-task"] != RepoSearchConsistencyOk {
		t.Fatalf("consistent-task = %v", consistency)
	}
	if consistency["mismatch-task"] != RepoSearchConsistencyMismatch {
		t.Fatalf("mismatch-task = %v", consistency)
	}
	if consistency["events-only-task"] != RepoSearchConsistencyStatsMissing {
		t.Fatalf("events-only-task = %v", consistency)
	}
	if consistency["legacy-task"] != RepoSearchConsistencyStatsMissing {
		t.Fatalf("legacy-task = %v", consistency)
	}
	if consistency["stats-only-task"] != RepoSearchConsistencyUnverified {
		t.Fatalf("stats-only-task = %v", consistency)
	}
	if report.Totals.Calls != 10 || report.Totals.Results != 25 || report.Totals.DurationMS != 4000 {
		t.Fatalf("totals = %+v", report.Totals)
	}
	if report.Totals.Hits != 5 || report.Totals.Fallbacks != 5 {
		t.Fatalf("totals class counts = %+v", report.Totals)
	}
	for _, task := range report.Tasks {
		if task.TaskID == "consistent-task" && (task.Review.Outcome != ModelRoutingQualityReviewPass || task.Measure.Calls != 2) {
			t.Fatalf("consistent-task summary = %+v", task)
		}
		if task.TaskID == "legacy-task" && (task.Measure.Calls != 2 || task.Measure.DurationMS != 800) {
			t.Fatalf("legacy-task summary = %+v", task)
		}
		if task.TaskID != "consistent-task" && task.Review.Outcome != TestImpactReviewOutcomeUnknown {
			t.Fatalf("review既定 = %+v", task.Review)
		}
	}
	if report.Evaluation.CodexReductionDelta != RepoSearchDeltaUnknown || report.Evaluation.QualityDelta != RepoSearchDeltaUnknown {
		t.Fatalf("evaluation = %+v", report.Evaluation)
	}
	joined := strings.Join(report.Evaluation.Reasons, "\n")
	if !strings.Contains(joined, "permission-gated live A/B") || !strings.Contains(joined, "mismatch 1") ||
		!strings.Contains(joined, "stats-missing 2") {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func TestBuildRepoSearchReportEmptyStateStaysUnknown(t *testing.T) {
	report := BuildRepoSearchReport(nil, map[string]TaskStats{}, nil)

	if len(report.Tasks) != 0 || report.Totals.Calls != 0 {
		t.Fatalf("tasks/totals = %+v", report)
	}
	if report.Evaluation.CodexReductionDelta != RepoSearchDeltaUnknown {
		t.Fatalf("codex reduction = %q", report.Evaluation.CodexReductionDelta)
	}
	joined := strings.Join(report.Evaluation.Reasons, "\n")
	if !strings.Contains(joined, "no repo-search route outcomes are recorded") {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func TestTaskStatsRejectsSameVersionWithoutCurrentSchemaRevision(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	obsolete := `{"version":3,"task_id":"obsolete-task","started_at":"2026-08-01T00:00:00Z","status":"active","model_calls":2}`
	if err := st.Write("task-stats.json", obsolete); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CurrentTaskStats(); err == nil || !strings.Contains(err.Error(), "unsupported task stats version") {
		t.Fatalf("same-version obsolete statsを拒否していません: %v", err)
	}
	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("unsupported current statsをaggregationからskipしていません: %+v", all)
	}
}

func sumIntMapForTest(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
