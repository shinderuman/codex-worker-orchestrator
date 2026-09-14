package state

import (
	"errors"
	"os"
	"testing"
)

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
	if _, err := os.Stat(st.TaskStatsArchivePath(firstTask)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed archive became visible: %v", err)
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
}
