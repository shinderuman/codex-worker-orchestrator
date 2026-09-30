package controller

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentTransitionPrepareAllowsOnlyOnePendingAuthority(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
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
	source, err := store.BootstrapExecution(task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}

	makeIntent := func() TransitionIntent {
		lease, leaseErr := store.prepareContinuationLease(source.Lease, source.Snapshot, source.Head.ControllerGeneration+3)
		if leaseErr != nil {
			t.Fatal(leaseErr)
		}
		target := authorityFromAdmission(source, source.Snapshot)
		target.LeaseID = lease.LeaseID
		return TransitionIntent{
			Kind:               "concurrent-prepare",
			ExpectedGeneration: source.Head.ControllerGeneration,
			Source:             source,
			Target:             target,
		}
	}
	intents := []TransitionIntent{makeIntent(), makeIntent()}
	start := make(chan struct{})
	results := make(chan error, len(intents))
	var wait sync.WaitGroup
	for _, intent := range intents {
		intent := intent
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, beginErr := store.BeginAuthorityTransition(intent)
			results <- beginErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	failures := 0
	for result := range results {
		if result == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent transition prepare results: successes=%d failures=%d", successes, failures)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID == "" || head.ControllerGeneration != source.Head.ControllerGeneration+1 {
		t.Fatalf("controller does not expose exactly one prepared transition: %#v", head)
	}
}

func TestFinalizingTransitionResumesFromDurableJournal(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	stateRoot := filepath.Join(t.TempDir(), "state", "sessions")
	cfg := controllerTestConfig(repo, stateRoot)
	store, err := Open(cfg)
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
	source, err := store.BootstrapExecution(task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	targetLease, err := store.prepareContinuationLease(source.Lease, source.Snapshot, source.Head.ControllerGeneration+3)
	if err != nil {
		t.Fatal(err)
	}
	target := authorityFromAdmission(source, source.Snapshot)
	target.LeaseID = targetLease.LeaseID
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/example", ExpectedOld: "old", ExpectedNew: "new"}
	record, err := store.BeginAuthorityTransition(TransitionIntent{
		Kind:               "crash-recovery",
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             target,
		Effects:            []EffectExpectation{effect},
	})
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{effect.Key(): "new"}
	if got := store.ClassifyTransition(record, map[string]string{effect.Key(): "old"})[effect.Key()]; got != EffectExpectedOld {
		t.Fatalf("prepared recovery old classification = %s", got)
	}
	if got := store.ClassifyTransition(record, actual)[effect.Key()]; got != EffectExpectedNew {
		t.Fatalf("prepared recovery new classification = %s", got)
	}
	if got := store.ClassifyTransition(record, map[string]string{effect.Key(): "foreign"})[effect.Key()]; got != EffectUnexpected {
		t.Fatalf("prepared recovery unexpected classification = %s", got)
	}

	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTransitionApplied(record, actual); err != nil {
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
	stateRecord, err := store.loadTransitionState(record.TransitionID)
	if err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	stateRecord.Phase = TransitionPhaseFinalizing
	if err := store.writeTransitionState(stateRecord); err != nil {
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
	if finalHead.ControllerGeneration != record.TargetGeneration || finalHead.PendingTransitionID != "" || finalHead.LiveLeaseID != targetLease.LeaseID {
		t.Fatalf("finalized controller head = %#v", finalHead)
	}
	_, stateRecord, err = reopened.LoadTransition(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if stateRecord.Phase != TransitionPhaseFinalized {
		t.Fatalf("resumed transition phase = %s", stateRecord.Phase)
	}
}
