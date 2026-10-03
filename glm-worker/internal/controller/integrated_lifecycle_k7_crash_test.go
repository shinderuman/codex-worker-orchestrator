package controller

import "testing"

func TestK7CrashRecoveryAfterPrepare(t *testing.T) {
	t.Parallel()
	cfg, _, record, targetLease, actual := prepareFinalizingRecoveryFixture(t)
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertK7TransitionRecoveryState(t, reopened, record, TransitionPhasePrepared, record.PreparedGeneration, true)
	finishK7RecoveredTransition(t, reopened, record, targetLease, actual, true)
}

func TestK7CrashRecoveryAfterApply(t *testing.T) {
	t.Parallel()
	cfg, store, record, targetLease, actual := prepareFinalizingRecoveryFixture(t)
	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, actual); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertK7TransitionRecoveryState(t, reopened, record, TransitionPhaseApplied, record.PreparedGeneration, true)
	finishK7RecoveredTransition(t, reopened, record, targetLease, actual, false)
}

func TestK7CrashRecoveryAfterCommit(t *testing.T) {
	t.Parallel()
	cfg, store, record, targetLease, actual := prepareFinalizingRecoveryFixture(t)
	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, actual); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if _, err := store.commitAuthorityTransitionLocked(record, actual, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	}); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertK7TransitionRecoveryState(t, reopened, record, TransitionPhaseCommitted, record.CommittedGeneration, true)
	lock, err = reopened.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	finalHead, err := reopened.finalizeAuthorityTransitionLocked(record)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	assertK7FinalizedTransition(t, reopened, record, targetLease, finalHead)
}

func finishK7RecoveredTransition(t *testing.T, store *Store, record TransitionRecord, targetLease ExecutionLease, actual map[string]string, markApplied bool) {
	t.Helper()
	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if markApplied {
		if err := store.markTransitionApplied(record, actual); err != nil {
			_ = lock.Close()
			t.Fatal(err)
		}
	}
	if _, err := store.commitAuthorityTransitionLocked(record, actual, false, func(next *RepositoryControllerHead) error {
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	}); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	finalHead, err := store.finalizeAuthorityTransitionLocked(record)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	assertK7FinalizedTransition(t, store, record, targetLease, finalHead)
}

func assertK7TransitionRecoveryState(t *testing.T, store *Store, record TransitionRecord, wantPhase TransitionPhase, wantGeneration uint64, wantPending bool) {
	t.Helper()
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	_, stateRecord, err := store.LoadTransition(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if stateRecord.Phase != wantPhase {
		t.Fatalf("recovered transition phase = %s, want %s", stateRecord.Phase, wantPhase)
	}
	if head.ControllerGeneration != wantGeneration {
		t.Fatalf("recovered controller generation = %d, want %d", head.ControllerGeneration, wantGeneration)
	}
	if wantPending && head.PendingTransitionID != record.TransitionID {
		t.Fatalf("recovered pending transition = %q, want %q", head.PendingTransitionID, record.TransitionID)
	}
}

func assertK7FinalizedTransition(t *testing.T, store *Store, record TransitionRecord, targetLease ExecutionLease, head RepositoryControllerHead) {
	t.Helper()
	if head.ControllerGeneration != record.TargetGeneration || head.PendingTransitionID != "" || head.LiveLeaseID != targetLease.LeaseID {
		t.Fatalf("finalized controller head = %#v", head)
	}
	_, stateRecord, err := store.LoadTransition(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if stateRecord.Phase != TransitionPhaseFinalized {
		t.Fatalf("finalized transition phase = %s", stateRecord.Phase)
	}
}
