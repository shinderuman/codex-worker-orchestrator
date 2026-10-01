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
	revision := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		2,
		"transition-2",
		time.Unix(3000, 0).UTC(),
	)
	taskRef, _, err := store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	headRef, ledgerRef := storeSingleTaskEvidenceAuthority(t, store, revision.TaskRef, taskRef, 2, "transition-2", "snapshot-2", time.Unix(3001, 0).UTC())
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

func TestEvidenceGraphFailsWhenRequiredTaskSemanticEvidenceIsMissing(t *testing.T) {
	fixture := newExecutionSwitchFixture(t)
	store := fixture.store
	findingRef, err := store.PutEvidenceObject("finding-record", "application/json", "finding:f1", true, []byte(`{"finding_id":"f1"}`))
	if err != nil {
		t.Fatal(err)
	}
	revision := testTaskIndexRevision(
		fixture.source.Attempt.SemanticTaskRef,
		2,
		"transition-2",
		time.Unix(3200, 0).UTC(),
	)
	revision.FindingRecords = []EvidenceObjectRef{findingRef}
	taskRef, _, err := store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	headRef, ledgerRef := storeSingleTaskEvidenceAuthority(t, store, revision.TaskRef, taskRef, 2, "transition-2", "snapshot-2", time.Unix(3201, 0).UTC())
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.evidenceObjectPath(findingRef.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err == nil {
		t.Fatal("missing required task semantic evidence was accepted")
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

func storeSingleTaskEvidenceAuthority(
	t *testing.T,
	store *Store,
	task SemanticTaskRef,
	taskRef EvidenceObjectRef,
	generation uint64,
	transitionID string,
	projectSnapshotID string,
	createdAt time.Time,
) (EvidenceObjectRef, EvidenceObjectRef) {
	t.Helper()
	headRef, _, err := store.StoreEvidenceHead(EvidenceHead{
		SchemaVersion:        evidenceSchemaVersion,
		RepositoryIdentity:   store.identity.LineageID,
		TaskHeads:            []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(task), RevisionRef: taskRef}},
		ControllerGeneration: generation,
		ProjectSnapshotID:    projectSnapshotID,
		CreatedAt:            createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRef, _, err := store.StoreEvidenceLedgerRecord(EvidenceLedgerRecord{
		SchemaVersion:        evidenceSchemaVersion,
		Sequence:             1,
		RepositoryIdentity:   store.identity.LineageID,
		ControllerGeneration: generation,
		TransitionID:         transitionID,
		ProjectSnapshotID:    projectSnapshotID,
		EvidenceHeadRef:      headRef,
		CreatedAt:            createdAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return headRef, ledgerRef
}
