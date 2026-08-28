package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRecoverableGuardFailurePersistsTaskCheckpoint(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	r := guardFailureRunner("done")
	w := newGitWorkflowT(t, st, r, repo)

	_, err := w.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("GuardRecoverableErrorを期待: %v", err)
	}
	checkpoint := retentionCheckpoint(t, st)
	if st.TaskStatus() != state.TaskStatusGuardRecoverable || !checkpoint.GuardRecoverable {
		t.Fatalf("guard recovery state = status:%s checkpoint:%#v", st.TaskStatus(), checkpoint)
	}
	if checkpoint.CompletedResult == nil || checkpoint.CompletedResult.Status != "IMPLEMENTED" {
		t.Fatalf("completed worker resultが保存されていません: %#v", checkpoint.CompletedResult)
	}
	if checkpoint.StopGitSnapshot == nil || checkpoint.StopGitSnapshot.Head == "" || checkpoint.StopDirtyFiles == nil {
		t.Fatalf("recovery retention baselineがありません: %#v", checkpoint)
	}
}

func TestGuardRecoveryReusesCompletedWorkerAfterRepairCommit(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopRunner := guardFailureRunner("done")
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}

	writeRetentionFile(t, filepath.Join(repo, "guard-repair.txt"), []byte("fixed\n"), 0o644)
	runRetentionGit(t, repo, "add", "guard-repair.txt")
	runRetentionGit(t, repo, "commit", "-q", "-m", "repair guard")

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("guard recovery resume failed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("workerを重複実行しました: phases=%v", resumeRunner.phases)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("successful recovery must clear checkpoint")
	}
}

func TestGuardRecoveryDirtyDriftFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	stopWorkflow := newGitWorkflowT(t, st, guardFailureRunner("done"), repo)
	if _, err := stopWorkflow.runModel(workerCheckpoint()); err == nil {
		t.Fatal("guard failure stopを期待")
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	err := resumeWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("dirty drift must fail closed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status = %s want guard-recoverable", st.TaskStatus())
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("dirty driftでmodel callを実行しました: %d", len(resumeRunner.prompts))
	}
	checkpoint := retentionCheckpoint(t, st)
	if !checkpoint.GuardRecoverable {
		t.Fatal("fail closed must preserve guard recovery checkpoint")
	}
}

func guardFailureRunner(summary string) *scriptedRunner {
	return &scriptedRunner{steps: []runnerStep{{
		structured: implementedPacket(summary),
		runErr: &runner.GitAuthorityGuardError{
			Stage:     "blocked-command",
			Mutations: []string{"command:branch"},
		},
	}}}
}
