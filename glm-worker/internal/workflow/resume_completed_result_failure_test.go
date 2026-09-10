package workflow

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResumeRetainsCompletedResultWhenRoutingFails(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopWorkflow := newGitWorkflowT(t, st, guardFailureRunner("done"), repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}
	before := retentionCheckpoint(t, st)
	if before.CompletedResult == nil {
		t.Fatal("reusable completed resultがありません")
	}
	if err := os.Mkdir(st.Path("last-review"), 0o700); err != nil {
		t.Fatal(err)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	if err := resumeWorkflow.ExecuteResume(); err == nil {
		t.Fatal("routing failureを期待")
	}

	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("task status = %s want guard-recoverable", st.TaskStatus())
	}
	after := retentionCheckpoint(t, st)
	if after.StopKind != state.ResumeStopGuardRecoverable || after.CompletedResult == nil {
		t.Fatalf("routing failure後にreusable checkpointが失われました: %#v", after)
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("reusable worker resultを再実行せずreviewへ進むべき: phases=%v", resumeRunner.phases)
	}
}
