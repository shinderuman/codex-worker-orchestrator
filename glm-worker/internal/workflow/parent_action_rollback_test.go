package workflow

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestDecisionPreCallGuardRollsBackToWaitingDecision(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingDecisionState(t, st)
	seedSessionState(t, st)

	r := &scriptedRunner{steps: []runnerStep{{runErr: beforeCallMismatchErr()}}}
	w := newGitWorkflowT(t, st, r, repo)

	err := w.ExecuteDecision("decision-body")
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "before-call-mismatch") {
		t.Fatalf("ExecuteDecision error = %v", err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("runner invocations = %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if _, checkpointErr := st.LoadResumeCheckpoint(); checkpointErr == nil {
		t.Fatal("rollback left a resume checkpoint")
	}
	if decision := st.ReadOr("last-decision", ""); decision != "decision-body" {
		t.Fatalf("last-decision = %q", decision)
	}
	assertSessionStateRetained(t, st)
	assertParentRollbackWorktreeClean(t, repo)
	plan, admitted, planErr := st.AdmitParentAction(state.ParentActionDecision)
	if planErr != nil || !admitted || plan.RequiredAction != state.ParentActionDecision {
		t.Fatalf("post-rollback admission = %#v admitted=%t err=%v", plan, admitted, planErr)
	}

	resendRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("decision resent")},
		{structured: needsSolReviewPacket()},
	}}
	resendWorkflow := newGitWorkflowT(t, st, resendRunner, repo)
	if err := resendWorkflow.ExecuteDecision("decision-body"); err != nil {
		t.Fatalf("resend decision failed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("resend state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if len(resendRunner.phases) != 2 || resendRunner.phases[0] != "worker-decision" {
		t.Fatalf("resend phases = %v", resendRunner.phases)
	}
}

func TestFixPreCallGuardRollsBackToWaitingSolReview(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	seedSessionState(t, st)

	r := &scriptedRunner{steps: []runnerStep{{runErr: beforeCallMismatchErr()}}}
	w := newGitWorkflowT(t, st, r, repo)

	err := w.ExecuteExplicitFix("fix instruction", "", "")
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "before-call-mismatch") {
		t.Fatalf("ExecuteExplicitFix error = %v", err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("runner invocations = %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || st.Exists("pending-decision") {
		t.Fatalf("rollback state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if _, checkpointErr := st.LoadResumeCheckpoint(); checkpointErr == nil {
		t.Fatal("rollback left a resume checkpoint")
	}
	if review := st.ReadOr("last-review", ""); review != "review-body" {
		t.Fatalf("last-review = %q", review)
	}
	assertSessionStateRetained(t, st)
	assertParentRollbackWorktreeClean(t, repo)
	plan, admitted, planErr := st.AdmitParentAction(state.ParentActionFix)
	if planErr != nil || !admitted {
		t.Fatalf("post-rollback admission = %#v admitted=%t err=%v", plan, admitted, planErr)
	}

	resendRunner := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("fix resent")},
		{structured: needsSolReviewPacket()},
	}}
	resendWorkflow := newGitWorkflowT(t, st, resendRunner, repo)
	if err := resendWorkflow.ExecuteExplicitFix("fix instruction", "", ""); err != nil {
		t.Fatalf("resend fix failed: %v", err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("resend state: status=%s", st.TaskStatus())
	}
	if len(resendRunner.phases) != 2 || resendRunner.phases[0] != "worker-explicit-fix" {
		t.Fatalf("resend phases = %v", resendRunner.phases)
	}
}

func TestDecisionPreCallGuardRollbackFailureFailsClosed(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingDecisionState(t, st)
	stateDir := filepath.Dir(st.Path("task.status"))

	r := &scriptedRunner{steps: []runnerStep{{runErr: beforeCallMismatchErr()}}}
	r.onRun = func() {
		if err := os.Chmod(stateDir, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
	w := newGitWorkflowT(t, st, r, repo)

	err := w.ExecuteDecision("decision-body")
	if err == nil ||
		!strings.Contains(err.Error(), "parent action begin failed and rollback failed") ||
		!strings.Contains(err.Error(), "before-call-mismatch") {
		t.Fatalf("rollback failure error = %v", err)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("failed rollback must not degrade the status: %s", st.TaskStatus())
	}
}

func TestDecisionSecondCallPreCallGuardDoesNotRollBackConsumedDecision(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingDecisionState(t, st)

	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("worker done")},
		{runErr: beforeCallMismatchErr()},
		{runErr: beforeCallMismatchErr()},
	}}
	w := newGitWorkflowT(t, st, r, repo)

	err := w.ExecuteDecision("decision-body")
	if err == nil || !strings.Contains(err.Error(), "before-call-mismatch") {
		t.Fatalf("ExecuteDecision error = %v", err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("runner invocations = %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("post-worker-call guard failure must not roll back to the consumed decision state: %s", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("post-worker-call guard failure must not resurrect pending-decision")
	}
}

func beforeCallMismatchErr() *runner.InstructionSurfaceGuardError {
	return &runner.InstructionSurfaceGuardError{
		Stage:        "before-call-mismatch",
		ChangedPaths: []string{"AGENTS.md/AGENTS.local.md"},
	}
}

func seedWaitingDecisionState(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
}

func seedWaitingSolReviewState(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "review-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
}

func seedSessionState(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.Write("worker.id", "worker-session"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("worker.ready"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("reviewer.id", "reviewer-session"); err != nil {
		t.Fatal(err)
	}
}

func assertSessionStateRetained(t *testing.T, st *state.StateStore) {
	t.Helper()
	if got := st.ReadOr("worker.id", ""); got != "worker-session" {
		t.Fatalf("worker.id = %q", got)
	}
	if !st.Exists("worker.ready") {
		t.Fatal("worker.ready was dropped")
	}
	if got := st.ReadOr("reviewer.id", ""); got != "reviewer-session" {
		t.Fatalf("reviewer.id = %q", got)
	}
}

func assertParentRollbackWorktreeClean(t *testing.T, repo string) {
	t.Helper()
	command := exec.Command("git", "-C", repo, "status", "--porcelain")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git status失敗: %v: %s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("rollbackがworking treeを変えました: %s", output)
	}
}
