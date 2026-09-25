package workflow

import (
	"errors"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteNewTaskPersistsParentCodexIdentityBeforeFirstDispatch(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{runErr: errors.New("worker stopped before terminal")},
	}}
	w := newWorkflowT(t, st, r)
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	t.Setenv(state.ParentActionCodexThreadIDEnv, threadID)
	t.Setenv(state.ParentActionCodexSessionIDEnv, threadID)

	_ = w.ExecuteNewTask("request")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != threadID || stats.ParentCodexSessionID != threadID {
		t.Fatalf("terminal前停止でもidentityが保存されている必要があります: %#v", stats)
	}
	if st.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("terminal前にtaskがcompleteになっています")
	}
}

func TestExecuteNewTaskWithoutParentActionIdentityLeavesStatsUnchanged(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{runErr: errors.New("worker stopped before terminal")},
	}}
	w := newWorkflowT(t, st, r)
	t.Setenv(state.ParentActionCodexThreadIDEnv, "")
	t.Setenv(state.ParentActionCodexSessionIDEnv, "")

	_ = w.ExecuteNewTask("request")
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != "" || stats.ParentCodexSessionID != "" {
		t.Fatalf("直接glm-worker実行でidentityが書き込まれています: %#v", stats)
	}
}
