package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestAcceptedFixScopeWaitingFixDoesNotAuthorizeParentAction(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if !st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("accepted fix scope was not captured")
	}
	if w.acceptedFixScopeContainsCurrent() {
		t.Fatal("waiting fix scope must not authorize quality-surface validation")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("waiting fix scope must not suppress the risk floor")
	}
}

func TestAcceptedFixScopeWaitingApprovalAllowsOnlyPreActivationValidation(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeForAction(acceptedFixScopeCurrentDiff, state.ParentActionApproveSurface); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		t.Fatal("same-owner quality approval must validate its accepted current diff")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("quality approval must not suppress the risk floor before activation")
	}
}

func TestAcceptedFixScopeRejectsDifferentParentOwner(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		t.Fatal("active fix must match its original task and parent lease")
	}
	if err := st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	if w.acceptedFixScopeContainsCurrent() {
		t.Fatal("scope from a previous parent lease must not remain authorized")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("active task must not consume scope from a previous parent lease")
	}
}

func TestAcceptedFixScopeRejectsDifferentTaskOwner(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", "different-task"); err != nil {
		t.Fatal(err)
	}
	if w.acceptedFixScopeContainsCurrent() {
		t.Fatal("scope from a different task identity must not remain authorized")
	}
}

func TestAcceptedFixScopeRejectsDifferentActionInvocation(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		t.Fatal("active fix must match its original action invocation")
	}
	w.temp = t.TempDir()
	if w.acceptedFixScopeContainsCurrent() {
		t.Fatal("scope from a previous action invocation must not remain authorized")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("later action invocation must not consume stale accepted scope")
	}
}

func TestAcceptedFixScopeExpiresWhenActionInvocationEnds(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeContainsCurrent() {
		t.Fatal("active invocation must own its accepted scope")
	}
	if err := os.RemoveAll(w.temp); err != nil {
		t.Fatal(err)
	}
	if w.acceptedFixScopeContainsCurrent() {
		t.Fatal("ended action invocation must not retain accepted scope authority")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("ended action invocation must not consume accepted scope")
	}
}

func TestAcceptedFixScopeBeginParentFixFailureDiscardsAuthorization(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	err := w.ExecuteExplicitFixWithExecutionMilestones(
		"fix instruction",
		"invalid-origin",
		state.ParentCauseWorker,
		acceptedFixScopeCurrentDiff,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown parent fix origin") {
		t.Fatalf("BeginParentFix failure = %v", err)
	}
	assertAcceptedScopeDiscarded(t, st, w)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status after failed BeginParentFix = %s", st.TaskStatus())
	}
}

func TestAcceptedFixScopePreCallRollbackDiscardsAuthorization(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	seedSessionState(t, st)
	writeAcceptedScopeChange(t, repo)

	r := &scriptedRunner{steps: []runnerStep{{runErr: beforeCallMismatchErr()}}}
	w := newGitWorkflowT(t, st, r, repo)
	err := w.ExecuteExplicitFixWithExecutionMilestones(
		"fix instruction",
		state.ParentOriginCodexReview,
		state.ParentCauseWorker,
		acceptedFixScopeCurrentDiff,
	)
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "before-call-mismatch") {
		t.Fatalf("pre-call failure = %v", err)
	}
	assertAcceptedScopeDiscarded(t, st, w)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status after pre-call rollback = %s", st.TaskStatus())
	}
}

func TestAcceptedFixScopeQualityApprovalValidationFailureDiscardsAuthorization(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	writeAcceptedScopeChange(t, repo)
	result := packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "implemented",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                         state.ResumeStageWorker,
		Phase:                         "worker-new",
		Role:                          state.WorkerRole,
		Model:                         "opus",
		Request:                       "request",
		QualitySurfaceApprovalPending: true,
		CompletedResult:               &result,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	if err := w.ExecuteQualitySurfaceApproval(acceptedFixScopeCurrentDiff); err == nil {
		t.Fatal("quality-surface approval without retained boundary must fail")
	}
	assertAcceptedScopeDiscarded(t, st, w)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status after failed quality-surface approval = %s", st.TaskStatus())
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("later unrelated active review consumed failed quality-surface authorization")
	}
}

func TestAcceptedFixScopeConsumesAuthorizationOnceAfterParentActionStarts(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeCoversCurrent() {
		t.Fatal("active parent fix must consume the accepted current-diff authorization")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("accepted scope authorization must be one-shot")
	}
	if st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("consumed accepted scope state was retained")
	}
}

func TestAcceptedApprovalScopeConsumesAuthorizationOnceAfterActivation(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	w.temp = t.TempDir()
	if err := w.prepareAcceptedFixScopeForAction(acceptedFixScopeCurrentDiff, state.ParentActionApproveSurface); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeCoversCurrent() {
		t.Fatal("active quality approval must consume its accepted current-diff authorization")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("quality approval authorization must be one-shot")
	}
	if st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("consumed quality approval scope state was retained")
	}
}

func writeAcceptedScopeChange(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "tracked.md"), []byte("base\naccepted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertAcceptedScopeDiscarded(t *testing.T, st *state.StateStore, w *Workflow) {
	t.Helper()
	if st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("accepted scope survived rollback")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("rolled-back accepted scope remained consumable")
	}
}
