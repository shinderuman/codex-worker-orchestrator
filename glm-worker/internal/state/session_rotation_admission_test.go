package state

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestAdmitNewTaskRequiresNewParentThreadWhenRotationIsPending(t *testing.T) {
	st, err := NewStateStore(config.AppConfig{StateBase: t.TempDir(), RepoHash: "rotation-admission", RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	oldThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	st.UpdateTaskStats(func(stats *TaskStats) {
		stats.ParentCodexThreadID = oldThread
	})
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: oldThread,
		TaskID:         taskID,
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonDefaultTwoTasks,
			Evidence: []SessionRotationEvidence{{Trigger: SessionRotationReasonDefaultTwoTasks}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, observed := range []string{"", oldThread} {
		if _, admitted, err := st.AdmitNewTaskForParentThread(observed); err == nil || admitted {
			t.Fatalf("pending rotation admitted observed thread %q: admitted=%v err=%v", observed, admitted, err)
		}
	}
	if _, admitted, err := st.AdmitNewTaskForParentThread(newThread); err != nil || !admitted {
		t.Fatalf("new parent thread was not admitted: admitted=%v err=%v", admitted, err)
	}
	marker, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil || marker == nil || marker.State != SessionRotationStatePending {
		t.Fatalf("admission check mutated rotation marker: marker=%#v err=%v", marker, err)
	}
}
