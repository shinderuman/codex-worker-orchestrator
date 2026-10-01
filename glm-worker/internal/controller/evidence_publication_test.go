package controller

import (
	"testing"
	"time"
)

func TestEvidencePublicationAdvancesWithTransitionCommitCAS(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record

	taskRevisionRef, _, err := store.StoreTaskIndexRevision(TaskIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		TaskRef:              fixture.source.Attempt.SemanticTaskRef,
		ControllerGeneration: record.CommittedGeneration,
		CreatedAt:            time.Unix(2000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, nil); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	committed, publication, err := store.commitAuthorityTransitionWithEvidenceLocked(
		record,
		nil,
		EvidencePublicationInput{TaskRevisionRefs: []EvidenceObjectRef{taskRevisionRef}},
		func(next *RepositoryControllerHead) error {
			root := record.TargetRootTaskRef
			execution := record.TargetExecutionTaskRef
			next.ProjectSnapshotID = record.ProjectSnapshotNew
			next.RootTaskRef = &root
			next.ExecutionTaskRef = &execution
			next.LiveAttemptID = record.TargetAttemptID
			next.LiveLeaseID = record.TargetLeaseID
			return nil
		},
	)
	if err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if committed.ControllerGeneration != record.CommittedGeneration {
		_ = lock.Close()
		t.Fatalf("evidence publication advanced controller generation outside COMMIT: got=%d want=%d", committed.ControllerGeneration, record.CommittedGeneration)
	}
	if committed.EvidenceHeadRef == nil || committed.EvidenceLedgerHeadRef == nil || committed.EvidenceLedgerSequence != 1 {
		_ = lock.Close()
		t.Fatalf("controller did not publish evidence roots atomically: %#v", committed)
	}
	if !evidenceRefsEqual(*committed.EvidenceHeadRef, publication.EvidenceHeadRef) ||
		!evidenceRefsEqual(*committed.EvidenceLedgerHeadRef, publication.LedgerRecordRef) {
		_ = lock.Close()
		t.Fatal("controller evidence roots differ from prepared immutable publication")
	}
	if publication.LedgerRecord.Sequence != 1 || publication.LedgerRecord.TransitionID != record.TransitionID ||
		publication.LedgerRecord.ControllerGeneration != record.CommittedGeneration {
		_ = lock.Close()
		t.Fatalf("ledger is not bound to transition COMMIT authority: %#v", publication.LedgerRecord)
	}
	if len(publication.EvidenceHead.TaskHeads) != 1 ||
		!evidenceRefsEqual(publication.EvidenceHead.TaskHeads[0].RevisionRef, taskRevisionRef) {
		_ = lock.Close()
		t.Fatalf("task revision was not published through evidence head: %#v", publication.EvidenceHead.TaskHeads)
	}

	finalHead, err := store.finalizeAuthorityTransitionLocked(record)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if finalHead.EvidenceHeadRef == nil || finalHead.EvidenceLedgerHeadRef == nil || finalHead.EvidenceLedgerSequence != 1 {
		t.Fatalf("FINALIZE lost committed evidence authority: %#v", finalHead)
	}
}

func TestEvidencePublicationRejectsUnpublishedIndexPredecessor(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record

	firstRef, _, err := store.StoreTaskIndexRevision(TaskIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		TaskRef:              fixture.source.Attempt.SemanticTaskRef,
		ControllerGeneration: record.CommittedGeneration,
		CreatedAt:            time.Unix(2000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRef, _, err := store.StoreTaskIndexRevision(TaskIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		TaskRef:              fixture.source.Attempt.SemanticTaskRef,
		PreviousRevision:     &firstRef,
		ControllerGeneration: record.CommittedGeneration,
		CreatedAt:            time.Unix(2001, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, nil); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	_, _, err = store.commitAuthorityTransitionWithEvidenceLocked(
		record,
		nil,
		EvidencePublicationInput{TaskRevisionRefs: []EvidenceObjectRef{secondRef}},
		nil,
	)
	closeErr := lock.Close()
	if err == nil {
		t.Fatal("unpublished task-index predecessor was accepted")
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	head, loadErr := store.LoadHead()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if head.ControllerGeneration != record.PreparedGeneration || head.EvidenceHeadRef != nil || head.EvidenceLedgerHeadRef != nil {
		t.Fatalf("rejected evidence publication changed controller authority: %#v", head)
	}
}
