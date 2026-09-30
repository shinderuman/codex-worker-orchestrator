package controller

import (
	"path/filepath"
	"testing"
)

func TestModelCallLeaseUsesJournalAndClearsInFlightBinding(t *testing.T) {
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

	bound, err := store.BindModelCall(source)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Lease.InFlightCallID == "" {
		t.Fatal("model call lease has no in-flight call identity")
	}
	if bound.Head.ControllerGeneration != source.Head.ControllerGeneration+3 {
		t.Fatalf("model call transition generation = %d want %d", bound.Head.ControllerGeneration, source.Head.ControllerGeneration+3)
	}
	if bound.Lease.ControllerGeneration != bound.Head.ControllerGeneration || bound.Head.LiveLeaseID != bound.Lease.LeaseID {
		t.Fatalf("model call lease is not the sole live authority: head=%#v lease=%#v", bound.Head, bound.Lease)
	}
	if _, err := store.AdmitMutation(source.Lease.SemanticTaskRef, source.Workspace, source.Snapshot); err == nil {
		t.Fatal("source lease remained admissible after model call binding")
	}

	paths, err := filepath.Glob(filepath.Join(store.dir, "transitions", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("model call transition records = %v err=%v", paths, err)
	}
	transitionID := filepath.Base(paths[0])
	transitionID = transitionID[:len(transitionID)-len(filepath.Ext(transitionID))]
	record, transitionState, err := store.LoadTransition(transitionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Kind != "model-call-admission" || transitionState.Phase != TransitionPhaseFinalized {
		t.Fatalf("model call journal not finalized: record=%#v state=%#v", record, transitionState)
	}
	if record.SourceLeaseID != source.Lease.LeaseID || record.TargetLeaseID != bound.Lease.LeaseID {
		t.Fatalf("model call journal does not bind exact lease transition: %#v", record)
	}

	completed, err := store.RecordAdmittedMutation(bound, "model:worker:test", "success", bound.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Lease.InFlightCallID != "" {
		t.Fatalf("completed model call retained in-flight identity: %#v", completed.Lease)
	}
	if completed.Lease.LeaseID == bound.Lease.LeaseID || completed.Head.ControllerGeneration != bound.Head.ControllerGeneration+1 {
		t.Fatalf("model call completion did not rotate lease authority: before=%#v after=%#v", bound, completed)
	}
}
