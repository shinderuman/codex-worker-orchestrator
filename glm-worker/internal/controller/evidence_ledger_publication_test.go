package controller

import (
	"os"
	"testing"
	"time"
)

func TestEvidencePublicationLedgerCapturesFindingAddition(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record
	findingRef, err := store.PutEvidenceObject(
		"finding-record",
		evidenceJSONMediaType,
		"finding:ledger-addition",
		true,
		[]byte(`{"finding_id":"ledger-addition"}`),
	)
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
	_, publication, err := store.commitAuthorityTransitionWithEvidenceLocked(
		record,
		nil,
		EvidencePublicationInput{FindingRecordRefs: []EvidenceObjectRef{findingRef}},
		nil,
	)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	ledger := publication.LedgerRecord
	if len(ledger.FindingRecordsAdded) != 1 || !evidenceRefsEqual(ledger.FindingRecordsAdded[0], findingRef) {
		t.Fatalf("ledger omitted exact finding addition: %#v", ledger.FindingRecordsAdded)
	}
	if len(ledger.ChangedTaskIndexHeads) != 0 || len(ledger.ChangedEpisodeIndexHeads) != 0 {
		t.Fatalf("finding-only transition invented index-head changes: %#v", ledger)
	}
	if !ledger.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("ledger created_at = %s want transition created_at %s", ledger.CreatedAt, record.CreatedAt)
	}
	if publication.EvidenceGraphDigest == "" {
		t.Fatal("finding-only publication did not produce evidence graph digest")
	}
	if err := os.Remove(store.evidenceObjectPath(findingRef.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(publication.LedgerRecordRef, publication.EvidenceHeadRef, ledger.Sequence); err == nil {
		t.Fatal("missing ledger-declared finding record was accepted")
	}
}

func TestEvidencePublicationRejectsFinalizationWrongControllerGeneration(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	record := fixture.record
	sealRef, err := store.PutEvidenceObject(
		"attempt-seal",
		evidenceJSONMediaType,
		"attempt:placeholder",
		true,
		[]byte(`{"placeholder":true}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	finalizationRef, _, err := store.StoreAttemptFinalization(AttemptFinalizationRecord{
		SchemaVersion:        evidenceSchemaVersion,
		AttemptSealRef:       sealRef,
		ControllerGeneration: record.CommittedGeneration + 1,
		TransitionID:         record.TransitionID,
		ProjectSnapshotID:    record.ProjectSnapshotNew,
		Kind:                 "integrated",
		CreatedAt:            time.Unix(9000, 0).UTC(),
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
		EvidencePublicationInput{FinalizationRefs: []EvidenceObjectRef{finalizationRef}},
		nil,
	)
	closeErr := lock.Close()
	if err == nil {
		t.Fatal("finalization bound to wrong controller generation was accepted")
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	head, loadErr := store.LoadHead()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if head.ControllerGeneration != record.PreparedGeneration || head.EvidenceHeadRef != nil || head.EvidenceLedgerHeadRef != nil {
		t.Fatalf("rejected finalization publication changed controller authority: %#v", head)
	}
}
