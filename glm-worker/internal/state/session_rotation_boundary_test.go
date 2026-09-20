package state

import "testing"

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
