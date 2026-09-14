package state

import (
	"errors"
	"os"
	"testing"
)

func TestArchiveCurrentStatsPreservesStoredRepoSearchAggregateWithoutEvents(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	seedStoredRepoSearchAggregate(t, st)
	removeTaskEventLog(t, st, taskID)

	st.ArchiveCurrentStats()

	archived := readArchivedTaskStats(t, st, taskID)
	requireStoredRepoSearchAggregate(t, archived)
}

func TestBestEffortTaskRotationPreservesStoredRepoSearchAggregateWithoutEvents(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	seedStoredRepoSearchAggregate(t, st)
	removeTaskEventLog(t, st, firstTask)

	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	archived := readArchivedTaskStats(t, st, firstTask)
	requireStoredRepoSearchAggregate(t, archived)
}

func TestRepoSearchArchiveWriteFailurePreservesLiveEvidence(t *testing.T) {
	st := newRepoSearchEvalTestStore(t)
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTaskEvent(TaskEventRecord{
		TaskID:      firstTask,
		Kind:        RepoSearchEventKind,
		Phase:       RepoSearchCategoryWorkerNavigation,
		Subtype:     RepoSearchOutcomeSearchHit,
		SearchPaths: []string{"kept.go"},
		DurationMS:  250,
	}); err != nil {
		t.Fatal(err)
	}
	st.UpdateTaskStats(func(stats *TaskStats) {
		stats.ParentReviewOpen = &ParentReviewOpenState{PacketStatus: "needs-sol-review"}
	})

	originalRemoveStatePath := removeStatePath
	removeStatePath = func(path string) error {
		if path == st.Path(currentStatsFile) {
			return errors.New("forced current task stats removal failure")
		}
		return originalRemoveStatePath(path)
	}
	defer func() { removeStatePath = originalRemoveStatePath }()

	if _, err := st.StartNewTask(); err == nil {
		t.Fatal("repo-search archive failure did not stop rotation")
	}
	currentTask, err := st.TaskID()
	if err != nil {
		t.Fatalf("archive failure lost current task ID: %v", err)
	}
	if currentTask != firstTask {
		t.Fatalf("archive failure committed new task ID: got %s want %s", currentTask, firstTask)
	}
	if _, err := os.Stat(st.TaskEventLogPath(firstTask)); err != nil {
		t.Fatalf("archive failure removed retained repo-search events: %v", err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatalf("archive failure removed current task stats: %v", err)
	}
	if stats.TaskID != firstTask {
		t.Fatalf("current task stats were replaced after archive failure: got %s want %s", stats.TaskID, firstTask)
	}
	if stats.ParentReviewOpen == nil {
		t.Fatal("archive failure resolved live parent review state")
	}
	if _, err := os.Stat(st.TaskStatsArchivePath(firstTask)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed archive became visible: %v", err)
	}
	if got := countUnknownParentCloseEvents(t, st, firstTask); got != 0 {
		t.Fatalf("archive failure leaked parent-close telemetry: got %d want 0", got)
	}

	removeStatePath = originalRemoveStatePath
	nextTask, err := st.StartNewTask()
	if err != nil {
		t.Fatalf("retry after archive rollback failed: %v", err)
	}
	if nextTask == firstTask {
		t.Fatalf("retry did not rotate task: %s", nextTask)
	}
	if _, err := os.Stat(st.TaskStatsArchivePath(firstTask)); err != nil {
		t.Fatalf("retry did not archive prior task stats: %v", err)
	}
	if got := countUnknownParentCloseEvents(t, st, firstTask); got != 1 {
		t.Fatalf("retry recorded unexpected parent-close telemetry count: got %d want 1", got)
	}
}

func seedStoredRepoSearchAggregate(t *testing.T, st *StateStore) {
	t.Helper()
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.RepoSearchCalls = 7
	stats.RepoSearchQueriesByCategory = map[string]int{
		RepoSearchCategoryWorkerNavigation:    4,
		RepoSearchCategoryReviewerIndependent: 3,
	}
	stats.RepoSearchOutcomes = map[string]int{
		RepoSearchOutcomeSearchHit:        5,
		RepoSearchOutcomeIndependentEmpty: 2,
	}
	stats.RepoSearchResults = 11
	stats.RepoSearchDurationMS = 1200
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}
}

func removeTaskEventLog(t *testing.T, st *StateStore, taskID string) {
	t.Helper()
	if err := os.Remove(st.TaskEventLogPath(taskID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func readArchivedTaskStats(t *testing.T, st *StateStore, taskID string) TaskStats {
	t.Helper()
	data, err := os.ReadFile(st.TaskStatsArchivePath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := decodeTaskStats(data)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func requireStoredRepoSearchAggregate(t *testing.T, stats TaskStats) {
	t.Helper()
	if stats.RepoSearchCalls != 7 || stats.RepoSearchResults != 11 || stats.RepoSearchDurationMS != 1200 {
		t.Fatalf("stored repo-search aggregate was not preserved: %+v", stats)
	}
	if stats.RepoSearchQueriesByCategory[RepoSearchCategoryWorkerNavigation] != 4 || stats.RepoSearchQueriesByCategory[RepoSearchCategoryReviewerIndependent] != 3 {
		t.Fatalf("stored repo-search query aggregate was not preserved: %+v", stats.RepoSearchQueriesByCategory)
	}
	if stats.RepoSearchOutcomes[RepoSearchOutcomeSearchHit] != 5 || stats.RepoSearchOutcomes[RepoSearchOutcomeIndependentEmpty] != 2 {
		t.Fatalf("stored repo-search outcome aggregate was not preserved: %+v", stats.RepoSearchOutcomes)
	}
}

func countUnknownParentCloseEvents(t *testing.T, st *StateStore, taskID string) int {
	t.Helper()
	logs, err := st.ReadModelCallLogs(taskID)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read model-call logs: %v", err)
	}
	count := 0
	for _, log := range logs {
		if log.CallType == CallTypeEvent && log.Phase == ParentPhaseClose && log.Outcome == ParentOutcomeUnknown {
			count++
		}
	}
	return count
}
