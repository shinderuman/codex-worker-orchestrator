package controller

import (
	"path/filepath"
	"testing"
)

func TestTransitionClassificationUsesExactOldAndNewState(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, source := bootstrapControllerTestExecution(t, repo)
	targetLease, err := store.prepareContinuationLease(source.Lease, source.Snapshot, source.Head.ControllerGeneration+3)
	if err != nil {
		t.Fatal(err)
	}
	target := authorityFromAdmission(source, source.Snapshot)
	target.LeaseID = targetLease.LeaseID
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/main", ExpectedOld: "old", ExpectedNew: "new"}
	record, err := store.BeginAuthorityTransition(TransitionIntent{
		Kind:               "test-ref-cas",
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             target,
		Effects:            []EffectExpectation{effect},
	})
	if err != nil {
		t.Fatal(err)
	}
	for actual, want := range map[string]EffectClassification{
		"old":   EffectExpectedOld,
		"new":   EffectExpectedNew,
		"other": EffectUnexpected,
	} {
		got := store.ClassifyTransition(record, map[string]string{effect.Key(): actual})[effect.Key()]
		if got != want {
			t.Fatalf("classify actual=%q got=%s want=%s", actual, got, want)
		}
	}
	if err := store.markTransitionApplied(record, map[string]string{effect.Key(): "new"}); err != nil {
		t.Fatal(err)
	}
	loaded, stateRecord, err := store.LoadTransition(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if stateRecord.Phase != TransitionPhaseApplied {
		t.Fatalf("transition phase = %s", stateRecord.Phase)
	}
	if got := store.ClassifyTransition(loaded, map[string]string{effect.Key(): "old"})[effect.Key()]; got != EffectExpectedOld {
		t.Fatalf("classification depended on progress state: %s", got)
	}
	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := store.commitAuthorityTransitionLocked(record, map[string]string{effect.Key(): "new"}, true, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	})
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if committed.PendingTransitionID != "" || committed.ControllerGeneration != record.TargetGeneration || committed.LiveLeaseID != targetLease.LeaseID {
		t.Fatalf("committed head = %#v", committed)
	}
}

func TestUnexpectedTransitionFailsClosedAndRevokesLease(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	store, source := bootstrapControllerTestExecution(t, repo)
	targetLease, err := store.prepareContinuationLease(source.Lease, source.Snapshot, source.Head.ControllerGeneration+3)
	if err != nil {
		t.Fatal(err)
	}
	target := authorityFromAdmission(source, source.Snapshot)
	target.LeaseID = targetLease.LeaseID
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/main", ExpectedOld: "old", ExpectedNew: "new"}
	record, err := store.BeginAuthorityTransition(TransitionIntent{
		Kind:               "test-unexpected",
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             target,
		Effects:            []EffectExpectation{effect},
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{effect.Key(): "foreign"}
	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.commitAuthorityTransitionLocked(record, actual, true, nil); err == nil {
		_ = lock.Close()
		t.Fatal("unexpected transition state committed")
	}
	failure, err := store.failClosedLocked("unexpected ref state", record.TransitionID, source.Workspace, source.Snapshot, source.Snapshot, actual)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != ControllerStatusFailClosed || head.LiveLeaseID != "" || head.FailureID != failure.FailureID {
		t.Fatalf("fail-closed head = %#v failure=%#v", head, failure)
	}
	if _, err := store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
		t.Fatal("fail-closed controller still admitted mutation")
	}
}

func bootstrapControllerTestExecution(t *testing.T, repo string) (*Store, Admission) {
	t.Helper()
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := controllerTestTask(t, repo)
	admission, err := store.BootstrapExecution(task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return store, admission
}
