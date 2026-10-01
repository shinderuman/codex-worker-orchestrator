package controller

import (
	"os"
	"testing"
	"time"
)

func TestEvidenceGraphDigestIsOrderIndependent(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	first, err := store.PutEvidenceObject("validation", "text/plain", "first", true, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutEvidenceObject("validation", "text/plain", "second", true, []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	left, err := evidenceGraphDigest(map[string]EvidenceObjectRef{
		evidenceRefKey(first):  first,
		evidenceRefKey(second): second,
	})
	if err != nil {
		t.Fatal(err)
	}
	right, err := evidenceGraphDigest(map[string]EvidenceObjectRef{
		evidenceRefKey(second): second,
		evidenceRefKey(first):  first,
	})
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("canonical graph digest depends on insertion order: left=%s right=%s", left, right)
	}
}

func TestEvidenceGraphFailsWhenRequiredIndexedObjectIsMissing(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	taskRef, _, err := store.StoreTaskIndexRevision(TaskIndexRevision{
		SchemaVersion:        evidenceSchemaVersion,
		TaskRef:              fixture.source.Attempt.SemanticTaskRef,
		ControllerGeneration: 2,
		CreatedAt:            time.Unix(3000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
		SchemaVersion:        evidenceSchemaVersion,
		RepositoryIdentity:   store.identity.LineageID,
		TaskHeads:            []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(fixture.source.Attempt.SemanticTaskRef), RevisionRef: taskRef}},
		ControllerGeneration: 2,
		ProjectSnapshotID:    "snapshot-2",
		CreatedAt:            time.Unix(3001, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion:        evidenceSchemaVersion,
		Sequence:             1,
		RepositoryIdentity:   store.identity.LineageID,
		ControllerGeneration: 2,
		TransitionID:         "transition-2",
		ProjectSnapshotID:    "snapshot-2",
		EvidenceHeadRef:      headRef,
		CreatedAt:            time.Unix(3002, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.evidenceObjectPath(taskRef.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err == nil {
		t.Fatal("missing required task index object was accepted")
	}
}

func TestEvidenceGraphRejectsBrokenLedgerSequence(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
		SchemaVersion:        evidenceSchemaVersion,
		RepositoryIdentity:   store.identity.LineageID,
		ControllerGeneration: 2,
		ProjectSnapshotID:    "snapshot-2",
		CreatedAt:            time.Unix(3100, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion:        evidenceSchemaVersion,
		Sequence:             2,
		RepositoryIdentity:   store.identity.LineageID,
		ControllerGeneration: 2,
		TransitionID:         "transition-2",
		ProjectSnapshotID:    "snapshot-2",
		EvidenceHeadRef:      headRef,
		CreatedAt:            time.Unix(3101, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 2); err == nil {
		t.Fatal("ledger sequence without predecessor was accepted")
	}
}
