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

func TestAcceptedFixScopePendingDoesNotAuthorizeWaitingParentAction(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if !st.Exists(acceptedFixScopePendingStateFile) {
		t.Fatal("accepted scope was not staged")
	}
	if st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("staged accepted scope became live before the parent action")
	}
	if !w.acceptedFixScopeContainsCurrent() {
		t.Fatal("staged scope must remain available for lifecycle validation")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("waiting parent action must not consume staged authorization")
	}
}

func TestAcceptedFixScopeBeginParentFixFailureDiscardsStagedAuthorization(t *testing.T) {
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

func TestAcceptedFixScopePreCallRollbackDiscardsStagedAuthorization(t *testing.T) {
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

func TestAcceptedFixScopeQualityApprovalValidationFailureDiscardsStagedAuthorization(t *testing.T) {
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

func TestAcceptedFixScopeConsumesStagedAuthorizationOnceAfterParentActionStarts(t *testing.T) {
	repo := newRetentionGitRepo(t)
	st := newGitStateStoreT(t, repo)
	seedWaitingSolReviewState(t, st)
	writeAcceptedScopeChange(t, repo)

	w := newGitWorkflowT(t, st, &scriptedRunner{}, repo)
	if err := w.prepareAcceptedFixScopeChecked(acceptedFixScopeCurrentDiff); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentFix(state.ParentOriginCodexReview, state.ParentCauseWorker); err != nil {
		t.Fatal(err)
	}
	if !w.acceptedFixScopeCoversCurrent() {
		t.Fatal("active parent fix must consume the staged current-diff authorization")
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("accepted scope authorization must be one-shot")
	}
	if st.Exists(acceptedFixScopePendingStateFile) || st.Exists(acceptedFixScopeStateFile) {
		t.Fatal("consumed accepted scope state was retained")
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
	if st.Exists(acceptedFixScopePendingStateFile) || st.Exists(acceptedFixScopeStateFile) {
		t.Fatalf("accepted scope survived rollback: pending=%t live=%t", st.Exists(acceptedFixScopePendingStateFile), st.Exists(acceptedFixScopeStateFile))
	}
	if w.acceptedFixScopeCoversCurrent() {
		t.Fatal("rolled-back accepted scope remained consumable")
	}
}
