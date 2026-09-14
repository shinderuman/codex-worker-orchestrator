package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestTaskRotationProjectsRepoSearchEventsIntoArchivedStats(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []TaskEventRecord{
		{TaskID: firstTask, Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeSearchHit, SearchPaths: []string{"a.go", "b.go", "c.go"}, DurationMS: 1500},
		{TaskID: firstTask, Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation, Subtype: RepoSearchOutcomeSearchEmptyFallback, DurationMS: 500},
		{TaskID: firstTask, Kind: RepoSearchEventKind, Phase: RepoSearchCategoryReviewerIndependent, Subtype: RepoSearchOutcomeIndependentErrorFallback, DurationMS: 900},
	} {
		if err := st.AppendTaskEvent(record); err != nil {
			t.Fatal(err)
		}
	}

	live, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if repoSearchStatsHaveRecordedRoutes(live) {
		t.Fatalf("live task statsにrepo-search mirrorが書き込まれています: %+v", live)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.TaskStatsArchivePath(firstTask))
	if err != nil {
		t.Fatal(err)
	}
	archived, err := decodeTaskStats(data)
	if err != nil {
		t.Fatal(err)
	}
	if archived.RepoSearchCalls != 3 || archived.RepoSearchResults != 3 || archived.RepoSearchDurationMS != 2900 {
		t.Fatalf("archived repo-search projection = %+v", archived)
	}
	if archived.RepoSearchQueriesByCategory[RepoSearchCategoryWorkerNavigation] != 2 || archived.RepoSearchQueriesByCategory[RepoSearchCategoryReviewerIndependent] != 1 {
		t.Fatalf("queries_by_category = %+v", archived.RepoSearchQueriesByCategory)
	}
	if archived.RepoSearchOutcomes[RepoSearchOutcomeSearchHit] != 1 || archived.RepoSearchOutcomes[RepoSearchOutcomeSearchEmptyFallback] != 1 || archived.RepoSearchOutcomes[RepoSearchOutcomeIndependentErrorFallback] != 1 {
		t.Fatalf("outcomes = %+v", archived.RepoSearchOutcomes)
	}
}

func TestUnreadableRepoSearchEventsStopRotationWithoutOverwritingEvidence(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.RepoSearchCalls = 7
	stats.RepoSearchQueriesByCategory = map[string]int{RepoSearchCategoryWorkerNavigation: 7}
	stats.RepoSearchOutcomes = map[string]int{RepoSearchOutcomeSearchHit: 7}
	stats.RepoSearchResults = 11
	stats.RepoSearchDurationMS = 1200
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}

	eventPath := st.TaskEventLogPath(firstTask)
	if err := os.MkdirAll(filepath.Dir(eventPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventPath, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err == nil || !strings.Contains(err.Error(), "repo-search archive投影") {
		t.Fatalf("malformed repo-search evidence did not stop rotation: %v", err)
	}
	currentTask, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if currentTask != firstTask {
		t.Fatalf("projection failure advanced task id: got %s want %s", currentTask, firstTask)
	}
	if _, err := os.Stat(st.TaskStatsArchivePath(firstTask)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("projection failure created stale archive: %v", err)
	}
	preserved, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if preserved.RepoSearchCalls != 7 || preserved.RepoSearchResults != 11 || preserved.RepoSearchDurationMS != 1200 {
		t.Fatalf("projection failure overwrote prior repo-search evidence: %+v", preserved)
	}
	if _, err := os.Stat(eventPath); err != nil {
		t.Fatalf("projection failure removed source event log: %v", err)
	}
}

func TestAllTaskStatsProjectsCurrentRepoSearchEventsWithoutPersistingLiveStats(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTaskEvent(TaskEventRecord{
		TaskID: taskID, Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation,
		Subtype: RepoSearchOutcomeSearchHit, SearchPaths: []string{"a.go", "b.go"}, DurationMS: 450,
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if repoSearchStatsHaveRecordedRoutes(raw) {
		t.Fatalf("current task stats were mutated before read projection: %+v", raw)
	}
	st.EnableRepoSearchReadProjection()
	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].TaskID != taskID {
		t.Fatalf("aggregate task stats = %+v", all)
	}
	if all[0].RepoSearchCalls != 1 || all[0].RepoSearchResults != 2 || all[0].RepoSearchDurationMS != 450 {
		t.Fatalf("current repo-search read projection = %+v", all[0])
	}
	persisted, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if repoSearchStatsHaveRecordedRoutes(persisted) {
		t.Fatalf("read projection wrote live repo-search counters: %+v", persisted)
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
	if measure.QueriesByCategory[RepoSearchCategoryWorkerNavigation] != 2 || measure.QueriesByCategory[RepoSearchCategoryReviewerIndependent] != 1 {
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

func TestBuildRepoSearchReportPrefersRetainedEventsAndFallsBackToArchivedStats(t *testing.T) {
	mismatchStats := repoSearchStatsFixture("mismatch-task")
	mismatchStats.RepoSearchResults = 9
	statsOnly := repoSearchStatsFixture("stats-only-task")
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
	}
	reviews := map[string]TestImpactReviewSummary{
		"consistent-task": {Outcome: ModelRoutingQualityReviewPass, PassCalls: 1},
	}

	report := BuildRepoSearchReport(events, statsByTask, reviews)
	if report.Retention != retainedTaskEventLogs {
		t.Fatalf("retention = %d", report.Retention)
	}
	if report.Totals.Calls != 10 || report.Totals.Results != 20 || report.Totals.DurationMS != 4000 {
		t.Fatalf("totals = %+v", report.Totals)
	}
	if report.Totals.Hits != 5 || report.Totals.Fallbacks != 5 {
		t.Fatalf("totals class counts = %+v", report.Totals)
	}
	for _, task := range report.Tasks {
		switch task.TaskID {
		case "consistent-task":
			if task.Review.Outcome != ModelRoutingQualityReviewPass || task.Measure.Calls != 2 {
				t.Fatalf("consistent-task summary = %+v", task)
			}
		case "mismatch-task":
			if task.Measure.Results != 4 {
				t.Fatalf("retained eventよりstats mirrorを優先しました: %+v", task)
			}
		case "stats-only-task":
			if task.Measure.Results != 4 || task.Measure.DurationMS != 800 {
				t.Fatalf("archived stats fallback = %+v", task)
			}
		}
		if task.TaskID != "consistent-task" && task.Review.Outcome != TestImpactReviewOutcomeUnknown {
			t.Fatalf("review既定 = %+v", task.Review)
		}
	}
	if report.Evaluation.CodexReductionDelta != RepoSearchDeltaUnknown || report.Evaluation.QualityDelta != RepoSearchDeltaUnknown {
		t.Fatalf("evaluation = %+v", report.Evaluation)
	}
	joined := strings.Join(report.Evaluation.Reasons, "\n")
	if !strings.Contains(joined, "permission-gated live A/B") || strings.Contains(joined, "mismatch") || strings.Contains(joined, "stats-missing") {
		t.Fatalf("reasons = %#v", report.Evaluation.Reasons)
	}
}

func TestBuildRepoSearchReportIncompleteEventsFallBackToArchivedStats(t *testing.T) {
	taskID := "partial-task"
	stats := repoSearchStatsFixture(taskID)
	partial := TaskEvents{TaskID: taskID, Records: []TaskEventRecord{{
		Kind: RepoSearchEventKind, Phase: RepoSearchCategoryWorkerNavigation,
		Subtype: RepoSearchOutcomeSearchHit, SearchPaths: []string{"only-visible.go"}, DurationMS: 100,
	}}}
	report := BuildRepoSearchReportWithCompleteness(
		[]TaskEvents{partial},
		map[string]TaskStats{taskID: stats},
		nil,
		map[string]bool{taskID: true},
	)
	if len(report.Tasks) != 1 {
		t.Fatalf("tasks = %+v", report.Tasks)
	}
	measure := report.Tasks[0].Measure
	if measure.Calls != stats.RepoSearchCalls || measure.Results != stats.RepoSearchResults || measure.DurationMS != stats.RepoSearchDurationMS {
		t.Fatalf("partial retained events overrode archived aggregate: %+v", measure)
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
