package state

import (
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func seedQualitySurfaceDecisionRun(t *testing.T, st *StateStore) {
	t.Helper()
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
}

func qualitySurfaceDecisionCheckpoint() ResumeCheckpoint {
	completed := packet.Result{Status: packet.StatusImplemented, Risk: packet.RiskLow}
	return ResumeCheckpoint{
		Stage:                         ResumeStageWorker,
		Phase:                         "worker-decision",
		Role:                          WorkerRole,
		Model:                         "opus",
		Request:                       "request",
		Decision:                      "decision-body",
		QualitySurfaceApprovalPending: true,
		CompletedResult:               &completed,
	}
}

func currentTaskID(t *testing.T, st *StateStore) string {
	t.Helper()
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	return taskID
}

func failWritesFor(t *testing.T, st *StateStore, name string) {
	t.Helper()
	original := writeFileAtomic
	path := st.Path(name)
	writeFileAtomic = func(target string, data []byte, mode os.FileMode) error {
		if target == path {
			return os.ErrPermission
		}
		return original(target, data, mode)
	}
	t.Cleanup(func() { writeFileAtomic = original })
}

func failRemovesFor(t *testing.T, st *StateStore, name string) {
	t.Helper()
	original := removeStatePath
	path := st.Path(name)
	removeStatePath = func(target string) error {
		if target == path {
			return os.ErrPermission
		}
		return original(target)
	}
	t.Cleanup(func() { removeStatePath = original })
}

func seedRetainedPreviousCheckpoint(t *testing.T, st *StateStore) {
	t.Helper()
	previous := qualitySurfaceDecisionCheckpoint()
	previous.QualitySurfaceApprovalPending = false
	previous.CompletedResult = nil
	previous.Model = "previous-run"
	if err := st.SaveResumeCheckpoint(previous); err != nil {
		t.Fatal(err)
	}
}

func assertNoRollbackFailure(t *testing.T, err error) {
	t.Helper()
	if strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("rollback itself failed: %v", err)
	}
}

func TestEnterQualitySurfaceApprovalWaitClearsResidualPendingDecision(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionRun(t, st)

	if err := st.EnterQualitySurfaceApprovalWait(qualitySurfaceDecisionCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("quality-surface approval waitがpending-decisionを行き止まり状態として残しました")
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil ||
		saved.CompletedResult.Status != packet.StatusImplemented || saved.Decision != "decision-body" {
		t.Fatalf("checkpoint = %#v", saved)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionApproveSurface || !plan.Allows(ParentActionFix) || !plan.Allows(ParentActionPark) {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.RequiredActionParameters["accepted-scope"] != "current-diff" {
		t.Fatalf("required action parameters = %#v", plan.RequiredActionParameters)
	}
}

func TestEnterQualitySurfaceApprovalWaitRejectsForeignOrigins(t *testing.T) {
	tests := []struct {
		name       string
		status     TaskStatus
		checkpoint func(ResumeCheckpoint) ResumeCheckpoint
	}{
		{
			name:   "task is not active",
			status: TaskStatusWaitingDecision,
			checkpoint: func(checkpoint ResumeCheckpoint) ResumeCheckpoint {
				return checkpoint
			},
		},
		{
			name:   "checkpoint has no approval binding",
			status: TaskStatusActive,
			checkpoint: func(checkpoint ResumeCheckpoint) ResumeCheckpoint {
				checkpoint.QualitySurfaceApprovalPending = false
				return checkpoint
			},
		},
		{
			name:   "checkpoint has no completed worker result",
			status: TaskStatusActive,
			checkpoint: func(checkpoint ResumeCheckpoint) ResumeCheckpoint {
				checkpoint.CompletedResult = nil
				return checkpoint
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLifecycleTestStore(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(test.status); err != nil {
				t.Fatal(err)
			}
			if err := st.Touch("pending-decision"); err != nil {
				t.Fatal(err)
			}
			if err := st.EnterQualitySurfaceApprovalWait(test.checkpoint(qualitySurfaceDecisionCheckpoint())); err == nil {
				t.Fatal("foreign origin must be rejected")
			}
			if st.TaskStatus() != test.status || !st.Exists("pending-decision") {
				t.Fatalf("rejected transition changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
			}
			if _, err := st.LoadResumeCheckpoint(); err == nil {
				t.Fatal("rejected transition published a resume checkpoint")
			}
		})
	}
}

func TestEnterQualitySurfaceApprovalWaitRollsBackOnLeaseAdvanceFailure(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionRun(t, st)
	seedRetainedPreviousCheckpoint(t, st)
	if err := st.Write(parentEvidenceLeasePath, "not-an-epoch"); err != nil {
		t.Fatal(err)
	}

	err := st.EnterQualitySurfaceApprovalWait(qualitySurfaceDecisionCheckpoint())
	if err == nil {
		t.Fatal("lease advance failure must fail the transition")
	}
	assertNoRollbackFailure(t, err)
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if !st.Exists("pending-decision") {
		t.Fatal("rollback did not restore the pending decision marker")
	}
	saved, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || saved.Model != "previous-run" {
		t.Fatalf("rollback did not restore the previous checkpoint: %#v err=%v", saved, loadErr)
	}
	if lease, readErr := st.Read(parentEvidenceLeasePath); readErr != nil || lease != "not-an-epoch" {
		t.Fatalf("rollback did not restore the lease: %q err=%v", lease, readErr)
	}
}

func TestEnterQualitySurfaceApprovalWaitRollsBackOnTaskStatusWriteFailure(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionRun(t, st)
	seedRetainedPreviousCheckpoint(t, st)
	if err := st.Write(parentEvidenceLeasePath, "3"); err != nil {
		t.Fatal(err)
	}
	failWritesFor(t, st, "task.status")

	err := st.EnterQualitySurfaceApprovalWait(qualitySurfaceDecisionCheckpoint())
	if err == nil {
		t.Fatal("task status write failure must fail the transition")
	}
	assertNoRollbackFailure(t, err)
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if !st.Exists("pending-decision") {
		t.Fatal("rollback did not restore the pending decision marker")
	}
	saved, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil || saved.Model != "previous-run" {
		t.Fatalf("rollback did not restore the previous checkpoint: %#v err=%v", saved, loadErr)
	}
	epoch, epochErr := st.ParentEvidenceLeaseEpoch()
	if epochErr != nil || epoch != 3 {
		t.Fatalf("rollback did not restore the advanced lease: epoch=%d err=%v", epoch, epochErr)
	}
}

func TestWaitForQualitySurfaceReviewClearsResidualPendingDecision(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedQualitySurfaceDecisionRun(t, st)

	if err := st.WaitForQualitySurfaceReview("worker-decision"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if st.Exists("pending-decision") {
		t.Fatal("quality-surface review waitがpending-decisionを行き止まり状態として残しました")
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionReview || !plan.Allows(ParentActionAccept) || !plan.Allows(ParentActionFix) {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestWaitForQualitySurfaceReviewAllowsPlainQualitySurfaceStop(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusActive); err != nil {
		t.Fatal(err)
	}

	if err := st.WaitForQualitySurfaceReview("worker-new"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
}

func TestWaitForQualitySurfaceReviewRejectsForeignPendingDecision(t *testing.T) {
	tests := []struct {
		name   string
		phase  string
		mutate func(t *testing.T, st *StateStore)
	}{
		{
			name:   "phase is outside the decision continuation",
			phase:  "worker-new",
			mutate: func(_ *testing.T, _ *StateStore) {},
		},
		{
			name:  "saved decision binding is missing",
			phase: "worker-decision",
			mutate: func(t *testing.T, st *StateStore) {
				if err := st.Remove("last-decision"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLifecycleTestStore(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			seedQualitySurfaceDecisionRun(t, st)
			test.mutate(t, st)

			if err := st.WaitForQualitySurfaceReview(test.phase); err == nil {
				t.Fatal("foreign pending decision must be rejected")
			}
			if st.TaskStatus() != TaskStatusActive || !st.Exists("pending-decision") {
				t.Fatalf("rejected wait changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
			}
		})
	}
}

func seedApprovedQualitySurfaceActivation(t *testing.T, st *StateStore) {
	t.Helper()
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(qualitySurfaceDecisionCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}
}

func TestActivateQualitySurfaceApprovalClosesOpenParentReview(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedApprovedQualitySurfaceActivation(t, st)

	if err := st.ActivateQualitySurfaceApproval(); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("activation must consume the approval checkpoint")
	}
	if label := st.OpenParentReviewLabel(); label != roundCommentNone {
		t.Fatalf("open parent review = %q want none", label)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeAccepted] != 1 || stats.ParentReviewOpen != nil {
		t.Fatalf("outcome集計 = outcomes:%#v open:%#v", stats.ParentOutcomes, stats.ParentReviewOpen)
	}
}

func TestActivateQualitySurfaceApprovalRejectsOpenDecisionReview(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(qualitySurfaceDecisionCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}

	if err := st.ActivateQualitySurfaceApproval(); err == nil {
		t.Fatal("open Sol decision review must block activation")
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("rejected activation changed the status: %s", st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err != nil {
		t.Fatal("rejected activation dropped the approval checkpoint")
	}
}

func TestActivateQualitySurfaceApprovalAbortsWhenReviewCloseFails(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedApprovedQualitySurfaceActivation(t, st)
	failWritesFor(t, st, parentReviewStateFile)

	err := st.ActivateQualitySurfaceApproval()
	if err == nil {
		t.Fatal("review close write failure must fail the activation")
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if _, loadErr := st.LoadResumeCheckpoint(); loadErr != nil {
		t.Fatalf("aborted activation dropped the approval checkpoint: %v", loadErr)
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("open parent review = %q want NEEDS_SOL_REVIEW", label)
	}
}

func assertActivationRolledBack(t *testing.T, st *StateStore) {
	t.Helper()
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("rollback did not reopen the closed parent review: %q", label)
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || !saved.QualitySurfaceApprovalPending {
		t.Fatalf("rollback did not retain the approval checkpoint: %#v err=%v", saved, err)
	}
	stats, statsErr := st.loadTaskStats()
	if statsErr != nil {
		t.Fatal(statsErr)
	}
	if stats.ParentOutcomes[ParentOutcomeAccepted] != 0 || stats.ParentReviewOpen == nil {
		t.Fatalf("rollback did not restore the outcome stats: outcomes:%#v open:%#v", stats.ParentOutcomes, stats.ParentReviewOpen)
	}
}

func TestActivateQualitySurfaceApprovalRollsBackOnCheckpointClearFailure(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedApprovedQualitySurfaceActivation(t, st)
	failRemovesFor(t, st, resumeStateFile)

	err := st.ActivateQualitySurfaceApproval()
	if err == nil {
		t.Fatal("checkpoint clear failure must fail the activation")
	}
	assertNoRollbackFailure(t, err)
	assertActivationRolledBack(t, st)
}

func TestActivateQualitySurfaceApprovalRollsBackOnTaskStatusWriteFailure(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	seedApprovedQualitySurfaceActivation(t, st)
	failWritesFor(t, st, "task.status")

	err := st.ActivateQualitySurfaceApproval()
	if err == nil {
		t.Fatal("task status write failure must fail the activation")
	}
	assertNoRollbackFailure(t, err)
	assertActivationRolledBack(t, st)
}

func TestRecoverQualitySurfaceDecisionWaitRepairsLeftoverMarker(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	taskID := currentTaskID(t, st)
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(qualitySurfaceDecisionCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("leftover marker must keep the task inconsistent before recovery")
	}
	if err := st.RecoverQualitySurfaceDecisionWait(taskID); err != nil {
		t.Fatal(err)
	}
	if st.Exists("pending-decision") {
		t.Fatal("recovery did not remove the leftover pending decision marker")
	}
	if st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("status = %s want waiting-sol-review", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || !saved.QualitySurfaceApprovalPending || saved.CompletedResult == nil ||
		saved.CompletedResult.Status != packet.StatusImplemented {
		t.Fatalf("recovery dropped the approval binding: %#v err=%v", saved, err)
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("open parent review = %q want NEEDS_SOL_REVIEW", label)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionApproveSurface || plan.RequiredActionParameters["accepted-scope"] != "current-diff" {
		t.Fatalf("recovered plan = %#v", plan)
	}
	if err := st.RecoverQualitySurfaceDecisionWait(taskID); err == nil {
		t.Fatal("recovery must be rejected once the canonical wait state is restored")
	}
}

func TestRecoverQualitySurfaceDecisionWaitRejectsTaskIDMismatch(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(qualitySurfaceDecisionCheckpoint()); err != nil {
		t.Fatal(err)
	}

	if err := st.RecoverQualitySurfaceDecisionWait("00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("task ID mismatch must be rejected")
	}
	if !st.Exists("pending-decision") || st.TaskStatus() != TaskStatusWaitingSolReview {
		t.Fatalf("rejected recovery changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
	}
}

func TestRecoverQualitySurfaceDecisionWaitRejectsForeignConditions(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		mutate func(t *testing.T, st *StateStore)
	}{
		{
			name:   "task is not waiting for Sol review",
			status: TaskStatusActive,
			mutate: func(_ *testing.T, _ *StateStore) {},
		},
		{
			name:   "completed worker result is not IMPLEMENTED",
			status: TaskStatusWaitingSolReview,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := qualitySurfaceDecisionCheckpoint()
				checkpoint.CompletedResult = &packet.Result{Status: packet.StatusFixRequired}
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "checkpoint phase is outside the decision continuation",
			status: TaskStatusWaitingSolReview,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := qualitySurfaceDecisionCheckpoint()
				checkpoint.Phase = "worker-new"
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "checkpoint has no quality approval binding",
			status: TaskStatusWaitingSolReview,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := qualitySurfaceDecisionCheckpoint()
				checkpoint.QualitySurfaceApprovalPending = false
				checkpoint.CompletedResult = nil
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "open parent review is a pending Sol decision",
			status: TaskStatusWaitingSolReview,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLifecycleTestStore(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(test.status); err != nil {
				t.Fatal(err)
			}
			if err := st.Touch("pending-decision"); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveResumeCheckpoint(qualitySurfaceDecisionCheckpoint()); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, st)

			if err := st.RecoverQualitySurfaceDecisionWait(currentTaskID(t, st)); err == nil {
				t.Fatal("foreign condition must be rejected")
			}
			if !st.Exists("pending-decision") || st.TaskStatus() != test.status {
				t.Fatalf("rejected recovery changed the state: status=%s pending=%t", st.TaskStatus(), st.Exists("pending-decision"))
			}
		})
	}
}

func stoppedAutoFixCheckpoint() ResumeCheckpoint {
	return ResumeCheckpoint{
		Stage:          ResumeStageAutoFix,
		Phase:          "worker-auto-fix-1",
		Role:           WorkerRole,
		Model:          "opus",
		Request:        "request",
		StopKind:       ResumeStopRateLimited,
		ResetAtRFC3339: "2026-09-09T00:00:00Z",
	}
}

func TestRecoverApprovedQualitySurfaceReviewClosesStaleReview(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(stoppedAutoFixCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ParentActionPlan(); err == nil {
		t.Fatal("stale open review must keep the stopped task inconsistent before recovery")
	}
	if err := st.RecoverApprovedQualitySurfaceReview(currentTaskID(t, st)); err != nil {
		t.Fatal(err)
	}
	if label := st.OpenParentReviewLabel(); label != roundCommentNone {
		t.Fatalf("open parent review = %q want none", label)
	}
	if st.TaskStatus() != TaskStatusRateLimited {
		t.Fatalf("status = %s want rate-limited", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || saved.StopKind != ResumeStopRateLimited || saved.Phase != "worker-auto-fix-1" {
		t.Fatalf("recovery dropped the stop checkpoint: %#v err=%v", saved, err)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != ParentActionResume || !plan.Allows(ParentActionResume) {
		t.Fatalf("recovered plan = %#v", plan)
	}
}

func TestRecoverApprovedQualitySurfaceReviewRejectsTaskIDMismatch(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(stoppedAutoFixCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}

	if err := st.RecoverApprovedQualitySurfaceReview("00000000-0000-4000-8000-000000000000"); err == nil {
		t.Fatal("task ID mismatch must be rejected")
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("rejected recovery changed the open parent review: %q", label)
	}
}

func TestRecoverApprovedQualitySurfaceReviewRejectsLeftoverPendingDecision(t *testing.T) {
	st := newLifecycleTestStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(stoppedAutoFixCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolReview, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}

	if err := st.RecoverApprovedQualitySurfaceReview(currentTaskID(t, st)); err == nil {
		t.Fatal("leftover pending decision must block the partial repair")
	}
	if label := st.OpenParentReviewLabel(); label != string(packet.StatusNeedsSolReview) {
		t.Fatalf("rejected recovery closed the open parent review: %q", label)
	}
	if !st.Exists("pending-decision") {
		t.Fatal("rejected recovery removed the leftover pending decision marker")
	}
}

func TestRecoverApprovedQualitySurfaceReviewRejectsForeignConditions(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		mutate func(t *testing.T, st *StateStore)
	}{
		{
			name:   "task is not stopped",
			status: TaskStatusWaitingSolReview,
			mutate: func(_ *testing.T, _ *StateStore) {},
		},
		{
			name:   "stop checkpoint does not match the status",
			status: TaskStatusRateLimited,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := stoppedAutoFixCheckpoint()
				checkpoint.StopKind = ResumeStopInterrupted
				checkpoint.ResetAtRFC3339 = ""
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "stopped checkpoint is on the worker decision continuation",
			status: TaskStatusRateLimited,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := stoppedAutoFixCheckpoint()
				checkpoint.Stage = ResumeStageWorker
				checkpoint.Phase = "worker-decision"
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "stopped checkpoint phase is an explicit fix",
			status: TaskStatusRateLimited,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				checkpoint := stoppedAutoFixCheckpoint()
				checkpoint.Phase = "worker-explicit-fix-1"
				if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "no stale parent review is open",
			status: TaskStatusRateLimited,
			mutate: func(_ *testing.T, _ *StateStore) {},
		},
		{
			name:   "open parent review is a pending Sol decision",
			status: TaskStatusRateLimited,
			mutate: func(t *testing.T, st *StateStore) {
				t.Helper()
				if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: "worker", Model: "opus"}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newLifecycleTestStore(t)
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(test.status); err != nil {
				t.Fatal(err)
			}
			if test.status != TaskStatusWaitingSolReview {
				if err := st.SaveResumeCheckpoint(stoppedAutoFixCheckpoint()); err != nil {
					t.Fatal(err)
				}
			}
			test.mutate(t, st)

			if err := st.RecoverApprovedQualitySurfaceReview(currentTaskID(t, st)); err == nil {
				t.Fatal("foreign condition must be rejected")
			}
			if st.TaskStatus() != test.status {
				t.Fatalf("rejected recovery changed the status: %s", st.TaskStatus())
			}
		})
	}
}
