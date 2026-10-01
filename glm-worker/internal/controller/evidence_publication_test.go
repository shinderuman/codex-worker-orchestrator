package controller

import (
	"testing"
	"time"
)

func TestTransitionCommitWithoutEvidenceUsesCorePath(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record

	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, nil); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	committed, err := store.commitAuthorityTransitionLocked(record, nil, false, nil)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if committed.ControllerGeneration != record.CommittedGeneration {
		t.Fatalf("core transition did not reach committed generation: got=%d want=%d", committed.ControllerGeneration, record.CommittedGeneration)
	}
	if committed.EvidenceHeadRef != nil || committed.EvidenceLedgerHeadRef != nil || committed.EvidenceLedgerSequence != 0 {
		t.Fatalf("empty evidence input unexpectedly created evidence authority: %#v", committed)
	}
}

func TestEvidencePublicationAdvancesWithTransitionCommitCAS(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record

	taskRevision := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		record.CommittedGeneration,
		record.TransitionID,
		time.Unix(2000, 0).UTC(),
	)

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
		EvidencePublicationInput{TaskRevisions: []TaskIndexRevision{taskRevision}},
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
	if len(publication.EvidenceHead.TaskHeads) != 1 {
		_ = lock.Close()
		t.Fatalf("task revision was not published through evidence head: %#v", publication.EvidenceHead.TaskHeads)
	}
	if publication.EvidenceGraphDigest == "" {
		_ = lock.Close()
		t.Fatal("evidence publication did not produce canonical graph digest")
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

	first := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		record.CommittedGeneration,
		record.TransitionID,
		time.Unix(2000, 0).UTC(),
	)
	firstRef, _, err := store.StoreTaskIndexRevision(first)
	if err != nil {
		t.Fatal(err)
	}
	second := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		record.CommittedGeneration,
		record.TransitionID,
		time.Unix(2001, 0).UTC(),
	)
	second.PreviousRevision = &firstRef
	secondRef, _, err := store.StoreTaskIndexRevision(second)
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

func testTaskIndexRevision(task SemanticTaskRef, generation uint64, transitionID string, createdAt time.Time) TaskIndexRevision {
	return TaskIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		TaskRef:              task,
		SemanticStatus:       "live",
		ControllerGeneration: generation,
		TransitionID:         transitionID,
		CreatedAt:            createdAt,
	}
}
