package controller

import (
	"os"
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
	if _, err := store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
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

func TestModelCallAdmissionRecoversThroughControllerOperation(t *testing.T) {
	for _, boundary := range []string{"prepared", "applied", "committed-head", "committed", "finalized-head"} {
		t.Run(boundary, func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			op, err := fixture.store.planModelCallAdmission(fixture.source)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.prepareExecutionOperation(&op, fixture.source.Head); err != nil {
				t.Fatal(err)
			}
			if boundary != "prepared" {
				if err := fixture.store.markTransitionApplied(op.Transition, nil); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "committed-head" || boundary == "committed" || boundary == "finalized-head" {
				if _, err := fixture.store.commitAuthorityTransitionLocked(op.Transition, nil, false, func(next *RepositoryControllerHead) error { next.LiveLeaseID = op.Lease.LeaseID; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "committed-head" {
				if err := fixture.store.writeTransitionState(TransitionState{SchemaVersion: controllerSchemaVersion, TransitionID: op.Transition.TransitionID, Phase: TransitionPhaseApplied}); err != nil {
					t.Fatal(err)
				}
			}
			if boundary == "finalized-head" {
				if _, err := fixture.store.finalizeAuthorityTransitionLocked(op.Transition); err != nil {
					t.Fatal(err)
				}
				if err := fixture.store.writeTransitionState(TransitionState{SchemaVersion: controllerSchemaVersion, TransitionID: op.Transition.TransitionID, Phase: TransitionPhaseFinalizing}); err != nil {
					t.Fatal(err)
				}
			}
			recovered, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
			if err != nil {
				t.Fatalf("model-call admission cannot recover at %s: %v", boundary, err)
			}
			if recovered.Admission == nil || recovered.Admission.Lease.LeaseID != op.Lease.LeaseID || recovered.Head.PendingTransitionID != "" || recovered.Head.ControllerGeneration != op.Transition.TargetGeneration {
				t.Fatalf("recovery has inconsistent admission: %#v", recovered)
			}
			if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
				t.Fatalf("repeated recovery failed: %v", err)
			}
			if _, err := fixture.store.AdmitMutation(fixture.source.MutationAuthority(), fixture.source.Workspace, fixture.source.Snapshot); err == nil {
				t.Fatal("recovery admitted revoked source lease")
			}
		})
	}
}

func TestModelCallAdmissionRecoveryPreservesUnexpectedWorkspace(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	op, err := fixture.store.planModelCallAdmission(fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, fixture.source.Head); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.source.Workspace.Root, "source.txt")
	changed := []byte("external change after prepare\n")
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("recovery admitted unexpectedly changed workspace")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID != op.Transition.TransitionID || head.ControllerGeneration != op.Transition.PreparedGeneration {
		t.Fatal("rejected recovery changed pending authority")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(changed) {
		t.Fatal("recovery discarded unexpected source bytes")
	}
}

func TestModelCallAdmissionRejectsAlreadyInFlightCall(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	bound, err := fixture.store.BindModelCall(fixture.source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.BindModelCall(bound); err == nil {
		t.Fatal("second model-call admission replaced an unresolved in-flight call")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != bound.Lease.LeaseID || head.ControllerGeneration != bound.Head.ControllerGeneration || head.PendingTransitionID != "" {
		t.Fatal("rejected model call changed live authority")
	}
}
