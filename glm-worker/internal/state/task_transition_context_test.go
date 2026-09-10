package state

import "testing"

func TestStartNewTaskClearsPreviousTaskContext(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	stale := []string{
		"active-task",
		"last-request",
		"last-decision",
		"pending-decision",
		"last-review",
		"baseline-status",
		"baseline-head",
		"baseline-worktree.patch",
		"baseline-index.patch",
		"accepted-fix-scope.json",
		ExecutionMilestonesStateFile,
		resumeStateFile,
	}
	for _, name := range stale {
		if err := st.Write(name, "stale"); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := st.Write(parentCodexIdentityFile, "stale"); err != nil {
		t.Fatal(err)
	}

	secondTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if secondTask == firstTask {
		t.Fatalf("task identity was reused: %s", secondTask)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s", st.TaskStatus())
	}
	for _, name := range append(stale, parentCodexIdentityFile) {
		if st.Exists(name) {
			t.Errorf("new task inherited %s", name)
		}
	}
}

func TestTaskResetIncludesNewTaskTransitionState(t *testing.T) {
	transition := make(map[string]bool)
	for _, name := range newTaskTransitionStateFileNames() {
		transition[name] = true
	}
	reset := make(map[string]bool)
	for _, name := range taskStateFileNames() {
		reset[name] = true
	}
	for name := range transition {
		if !reset[name] {
			t.Errorf("task reset omits transition state %s", name)
		}
	}
}
