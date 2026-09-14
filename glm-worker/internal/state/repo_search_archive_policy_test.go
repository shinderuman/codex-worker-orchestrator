package state

import (
	"errors"
	"os"
	"path/filepath"
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

	statsDir := filepath.Join(st.dir, "stats")
	if err := os.MkdirAll(statsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(statsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(statsDir, 0o700) }()

	if _, err := st.StartNewTask(); err == nil {
		t.Fatal("repo-search archive write failure did not stop rotation")
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
}
