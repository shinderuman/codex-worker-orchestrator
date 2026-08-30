package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRefMutationGuardStopPersistsEvidenceAndRequiresExactRepair(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	beforeDigest, err := runner.CaptureGitAuthorityRefDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	refErr := &runner.GitAuthorityGuardError{
		Stage:           "after-call-mutation",
		Mutations:       []string{"refs"},
		RefBeforeDigest: beforeDigest,
		RefAfterDigest:  "different-after-digest",
		RefChanges: []runner.GitRefChange{{
			Name:  "refs/heads/bypass",
			After: &runner.GitRefState{Name: "refs/heads/bypass", ObjectID: "after-object"},
		}},
	}
	stopRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done"), runErr: refErr}}}
	stopWorkflow := newGitWorkflowT(t, st, stopRunner, repo)
	_, err = stopWorkflow.runModel(workerCheckpoint())
	var stopped *GuardRecoverableError
	if !errors.As(err, &stopped) {
		t.Fatalf("refs-only mutation should enter guard recovery: %v", err)
	}
	checkpoint := retentionCheckpoint(t, st)
	if checkpoint.GuardRefBeforeDigest != beforeDigest || checkpoint.GuardRefAfterDigest != "different-after-digest" || len(checkpoint.GuardRefChanges) != 1 {
		t.Fatalf("ref evidence not retained: %#v", checkpoint)
	}
	if checkpoint.CompletedResult == nil {
		t.Fatal("completed worker result should remain reusable after exact ref repair")
	}

	runRetentionGit(t, repo, "branch", "bypass")
	blockedRunner := &scriptedRunner{}
	blockedWorkflow := newGitWorkflowT(t, st, blockedRunner, repo)
	err = blockedWorkflow.ExecuteResume()
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(workerErr.Message, "refs are not restored") {
		t.Fatalf("unrepaired refs must fail closed: %v", err)
	}
	if len(blockedRunner.prompts) != 0 {
		t.Fatalf("unrepaired refs dispatched model calls: %d", len(blockedRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusGuardRecoverable {
		t.Fatalf("status after rejected resume = %s", st.TaskStatus())
	}

	runRetentionGit(t, repo, "branch", "-D", "bypass")
	repairedDigest, err := runner.CaptureGitAuthorityRefDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	if repairedDigest != beforeDigest {
		t.Fatalf("fixture did not restore exact refs: got %s want %s", repairedDigest, beforeDigest)
	}
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	resumeWorkflow := newGitWorkflowT(t, st, resumeRunner, repo)
	resumeWorkflow.collectChangedPaths = func(string, string) ([]string, error) { return nil, nil }
	if err := resumeWorkflow.ExecuteResume(); err != nil {
		t.Fatalf("repaired refs should resume same task: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("status = %s want complete", st.TaskStatus())
	}
	if len(resumeRunner.phases) != 1 || resumeRunner.phases[0] != "reviewer-1" {
		t.Fatalf("completed worker call was duplicated: phases=%v", resumeRunner.phases)
	}
}
