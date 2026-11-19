package state

import (
	"os"
	"testing"
)

func TestAdmitNewTaskRotationBoundaryDeclinesPendingRecommendationAfterAdmission(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	ordinaryThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         taskID,
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonDefaultTwoTasks,
		},
	}); err != nil {
		t.Fatal(err)
	}

	resume, err := st.AdmitNewTaskRotationBoundary(ordinaryThread, "")
	if err != nil || resume {
		t.Fatalf("ordinary start admission = resume:%v err:%v", resume, err)
	}
	beforeRetire, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if beforeRetire == nil || beforeRetire.State != SessionRotationStatePending || beforeRetire.Directive == nil {
		t.Fatalf("admission mutated pending recommendation before ordinary start was accepted: %#v", beforeRetire)
	}
	if err := st.StagePendingSessionRotationRecommendationRetirement(ordinaryThread); err != nil {
		t.Fatal(err)
	}
	staged, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if staged == nil || staged.State != SessionRotationStatePending || staged.Directive == nil {
		t.Fatalf("retirement staging mutated pending recommendation: %#v", staged)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if marker == nil || marker.State != "" || marker.Directive != nil {
		t.Fatalf("pending recommendation was not retired: %#v", marker)
	}
	if marker.LastEvaluation == nil || !marker.LastEvaluation.Required || marker.LastEvaluation.TaskID != taskID {
		t.Fatalf("last evaluation evidence was not preserved: %#v", marker.LastEvaluation)
	}
	if st.Exists(pendingSessionRotationRetirementStateFile) {
		t.Fatal("retirement staging survived committed ordinary task start")
	}
}

func TestPendingRecommendationRetirementRevalidatesBoundaryBeforeTaskCommit(t *testing.T) {
	st := newAtomicTransitionFixture(t)
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	ordinaryThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         oldAtomicTaskID,
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonDefaultTwoTasks,
		},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StagePendingSessionRotationRecommendationRetirement(ordinaryThread); err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.startNewTaskWithID(newAtomicTaskID, false); err == nil {
		t.Fatal("ordinary task transition bypassed a newly claimed rotation")
	}
	if current, err := st.TaskID(); err != nil || current != oldAtomicTaskID {
		t.Fatalf("task transition changed after boundary revalidation failure: task=%q err=%v", current, err)
	}
	after, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if after == nil || after.State != SessionRotationStateClaimed || after.Claim == nil || after.Claim.ClaimID != claim.ClaimID {
		t.Fatalf("claimed transaction changed after boundary revalidation failure: %#v", after)
	}
	if !st.Exists(pendingSessionRotationRetirementStateFile) {
		t.Fatal("retirement staging disappeared after pre-commit boundary failure")
	}
}

func TestPendingRecommendationRetirementRollsBackWithNewTaskTransition(t *testing.T) {
	st := newAtomicTransitionFixture(t)
	ordinaryThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	parentThreads := []string{
		"01a0463c-d477-7410-9efd-cb34ff2e0b0e",
		"01b0463c-d477-7410-9efd-cb34ff2e0b0e",
	}
	for _, parentThread := range parentThreads {
		if err := st.commitSessionRotation(&SessionRotationEvaluation{
			ParentThreadID: parentThread,
			TaskID:         oldAtomicTaskID,
			Terminal:       SessionRotationTerminalAccept,
			Decision: SessionRotationDecision{
				Required: true,
				Reason:   SessionRotationReasonDefaultTwoTasks,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.StagePendingSessionRotationRecommendationRetirement(ordinaryThread); err != nil {
		t.Fatal(err)
	}
	stage, err := st.loadPendingSessionRotationRecommendationRetirement()
	if err != nil {
		t.Fatal(err)
	}
	if stage == nil || len(stage.Recommendations) != len(parentThreads) {
		t.Fatalf("retirement stage = %#v", stage)
	}
	failThread := stage.Recommendations[len(stage.Recommendations)-1].ParentThreadID
	injectOneNewTaskWriteFailure(t, st.SessionRotationMarkerPath(failThread))

	if _, err := st.startNewTaskWithID(newAtomicTaskID, false); err == nil {
		t.Fatal("new task transition unexpectedly succeeded")
	}
	if current, err := st.TaskID(); err != nil || current != oldAtomicTaskID {
		t.Fatalf("task transition was not rolled back: task=%q err=%v", current, err)
	}
	for _, parentThread := range parentThreads {
		marker, err := st.LoadSessionRotationMarker(parentThread)
		if err != nil {
			t.Fatal(err)
		}
		if marker == nil || marker.State != SessionRotationStatePending || marker.Directive == nil {
			t.Fatalf("pending recommendation was not rolled back for %s: %#v", parentThread, marker)
		}
	}
	if !st.Exists(pendingSessionRotationRetirementStateFile) {
		t.Fatal("retirement staging was not restored for retry")
	}
}

func TestAdmitNewTaskRotationBoundaryKeepsClaimedTransactionFailClosed(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	ordinaryThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonCompaction,
		},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.AdmitNewTaskRotationBoundary(ordinaryThread, ""); err == nil {
		t.Fatal("ordinary start bypassed a claimed rotation transaction")
	}
	after, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != SessionRotationStateClaimed || after.Claim == nil || after.Claim.ClaimID != claim.ClaimID {
		t.Fatalf("claimed transaction changed after rejected ordinary start: %#v", after)
	}
}

func TestAdmitNewTaskRotationBoundaryPreservesClaimedStartPath(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	boundThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	sourceTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         sourceTask,
		Terminal:       SessionRotationTerminalAccept,
		Decision: SessionRotationDecision{
			Required: true,
			Reason:   SessionRotationReasonHighRisk,
		},
	}); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(parentThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.BindSessionRotationClaim(parentThread, marker.Directive.DirectiveID, claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}

	resume, err := st.AdmitNewTaskRotationBoundary(boundThread, claim.ClaimID)
	if err != nil || resume {
		t.Fatalf("claimed rotation admission = resume:%v err:%v", resume, err)
	}
}

func TestCanonicalRotationAdmissionMatchesStartWithNoPreviousTask(t *testing.T) {
	st, parentThread, directiveID, claim := seedClaimedSessionRotationIdentityTest(t)
	boundThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.BindSessionRotationClaim(parentThread, directiveID, claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}
	if st.Exists("task.id") {
		t.Fatal("fixture unexpectedly contains a task")
	}
	if resume, err := st.AdmitNewTaskRotationBoundary(boundThread, claim.ClaimID); err != nil || resume {
		t.Fatalf("bound admission without old task = resume:%v err:%v", resume, err)
	}
	taskID, err := st.StartSessionRotationTask(boundThread, claim.ClaimID)
	if err != nil || taskID != claim.TargetTaskID {
		t.Fatalf("bound start without old task = %q err:%v", taskID, err)
	}
}

func TestCanonicalRotationRejectsUnreadableOrEmptyTaskIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*StateStore) error
	}{
		{name: "directory", prepare: func(st *StateStore) error { return os.Mkdir(st.Path("task.id"), 0o700) }},
		{name: "empty file", prepare: func(st *StateStore) error { return st.Write("task.id", "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, parentThread, directiveID, claim := seedClaimedSessionRotationIdentityTest(t)
			boundThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
			if err := st.BindSessionRotationClaim(parentThread, directiveID, claim.ClaimID, boundThread); err != nil {
				t.Fatal(err)
			}
			if err := tc.prepare(st); err != nil {
				t.Fatal(err)
			}
			if _, err := st.AdmitNewTaskRotationBoundary(boundThread, claim.ClaimID); err == nil {
				t.Fatal("preflight admitted an invalid existing task identity")
			}
			if _, err := st.StartSessionRotationTask(boundThread, claim.ClaimID); err == nil {
				t.Fatal("rotation start admitted an invalid existing task identity")
			}
			if st.Exists("task.status") {
				t.Fatal("denied start modified the task runtime")
			}
			marker, err := st.LoadSessionRotationMarker(parentThread)
			if err != nil || marker == nil || marker.State != SessionRotationStateBound {
				t.Fatalf("denied start changed the bound claim: %#v err=%v", marker, err)
			}
		})
	}
}

func TestCanonicalRotationStartRechecksClaimAfterPreflight(t *testing.T) {
	st, parentThread, directiveID, claim := seedClaimedSessionRotationIdentityTest(t)
	boundThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.BindSessionRotationClaim(parentThread, directiveID, claim.ClaimID, boundThread); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitNewTaskRotationBoundary(boundThread, claim.ClaimID); err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(parentThread)
	if err != nil {
		t.Fatal(err)
	}
	marker.Claim.BoundThreadID = "01a08268-2c7d-72a0-9aa1-605eed066ce9"
	if err := st.writeSessionRotationMarker(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartSessionRotationTask(boundThread, claim.ClaimID); err == nil {
		t.Fatal("stale preflight authorized a different bound thread")
	}
	if st.Exists("task.id") {
		t.Fatal("denied mutation created a task")
	}
}

func TestPendingRetirementSurvivesRestartBeforeCommit(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parentThread := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	ordinaryThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.commitSessionRotation(&SessionRotationEvaluation{
		ParentThreadID: parentThread,
		TaskID:         "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Terminal:       SessionRotationTerminalAccept,
		Decision:       SessionRotationDecision{Required: true, Reason: SessionRotationReasonCompaction},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.StagePendingSessionRotationRecommendationRetirement(ordinaryThread); err != nil {
		t.Fatal(err)
	}
	restarted := &StateStore{dir: st.dir}
	if _, err := restarted.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	marker, err := restarted.LoadSessionRotationMarker(parentThread)
	if err != nil || marker == nil || marker.State != "" || marker.Directive != nil {
		t.Fatalf("restarted retirement = %#v err:%v", marker, err)
	}
	if restarted.Exists(pendingSessionRotationRetirementStateFile) {
		t.Fatal("completed retirement left a staging journal after restart")
	}
}
