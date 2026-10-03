package controller

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestConcurrentModelCallAdmissionAllowsOnlyOneLiveCall(t *testing.T) {
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

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, bindErr := store.BindModelCall(source)
			results <- bindErr
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
	if head.PendingTransitionID != "" || head.ControllerGeneration != source.Head.ControllerGeneration+3 || head.LiveLeaseID == source.Lease.LeaseID {
		t.Fatalf("concurrent admission lost live authority: %#v", head)
	}
	lease, err := store.loadLease(head.LiveLeaseID)
	if err != nil || lease.InFlightCallID == "" {
		t.Fatal("concurrent admission has no in-flight call identity")
	}
}

func TestFinalizingTransitionResumesFromDurableJournal(t *testing.T) {
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

func prepareFinalizingRecoveryFixture(t *testing.T) (cfg config.AppConfig, store *Store, record TransitionRecord, targetLease ExecutionLease, actual map[string]string) {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	cfg = controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))
	var err error
	store, err = Open(cfg)
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
	targetLease, err = store.prepareContinuationLease(source.Lease, source.Snapshot, source.Head.ControllerGeneration+3)
	if err != nil {
		t.Fatal(err)
	}
	target := authorityFromAdmission(source, source.Snapshot)
	target.LeaseID = targetLease.LeaseID
	effect := EffectExpectation{Surface: MutationSurfaceRef, Resource: "refs/heads/example", ExpectedOld: "old", ExpectedNew: "new"}
	record, err = prepareTestAuthorityTransition(store, TransitionIntent{
		Kind:               "crash-recovery",
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             target,
		Effects:            []EffectExpectation{effect},
	})
	if err != nil {
		t.Fatal(err)
	}
	actual = map[string]string{effect.Key(): "new"}
	return cfg, store, record, targetLease, actual
}
