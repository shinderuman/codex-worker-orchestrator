package controller

import (
	"os"
	"testing"
	"time"
)

func TestAttemptSealPortableAuthorityRejectsIncompleteState(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	base := validPortableAttemptSeal(t, store, repo)
	cases := map[string]func(*AttemptSeal){
		"missing source snapshot": func(record *AttemptSeal) {
			record.SourceProjectSnapshotID = ""
		},
		"sealed before start": func(record *AttemptSeal) {
			record.SealedAt = record.StartedAt.Add(-time.Second)
		},
		"episode revision without episode": func(record *AttemptSeal) {
			record.EpisodeRevision = 1
		},
		"partial candidate authority": func(record *AttemptSeal) {
			record.CandidateCommitOID = record.ExecutionBaseOID
		},
		"invalid coverage": func(record *AttemptSeal) {
			record.Coverage = "unknown"
		},
		"complete coverage with missing evidence": func(record *AttemptSeal) {
			record.Missing = []string{"telemetry"}
		},
		"archive roots omit workspace tree": func(record *AttemptSeal) {
			record.GitObjectArchiveRoots = []GitObjectArchiveRoot{{OID: record.ExecutionBaseOID, Type: "commit"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			record := base
			record.GitObjectArchiveRoots = append([]GitObjectArchiveRoot(nil), base.GitObjectArchiveRoots...)
			mutate(&record)
			if _, _, err := store.StoreAttemptSeal(record); err == nil {
				t.Fatalf("invalid portable AttemptSeal was accepted: %#v", record)
			}
		})
	}
}

func TestAttemptSealCanonicalIdentityIncludesDedicatedEvidenceWithoutOrderDependence(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	record := validPortableAttemptSeal(t, store, repo)
	findingA := putAttemptSealTestEvidence(t, store, "finding-record", "finding:a")
	findingB := putAttemptSealTestEvidence(t, store, "finding-record", "finding:b")
	review := putAttemptSealTestEvidence(t, store, "validation", "review:a")
	session := putAttemptSealTestEvidence(t, store, "session-association", "session:a")
	record.FindingRecordRefs = []EvidenceObjectRef{findingB, findingA}
	record.ReviewValidationRefs = []EvidenceObjectRef{review}
	record.SessionAssociationRefs = []EvidenceObjectRef{session}
	firstRef, first, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	record.FindingRecordRefs = []EvidenceObjectRef{findingA, findingB}
	record.RequiredKinds = []string{"session-association", "finding-record", "validation", "git-object-archive"}
	secondRef, second, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	if firstRef.Digest != secondRef.Digest || first.AttemptSealID != second.AttemptSealID {
		t.Fatalf("AttemptSeal identity depends on evidence ordering: first=%s second=%s", firstRef.Digest, secondRef.Digest)
	}
	for _, ref := range []EvidenceObjectRef{findingA, findingB, review, session} {
		if !containsEvidenceRef(first.EvidenceRefs, ref) {
			t.Fatalf("dedicated evidence ref was not included in canonical graph refs: %#v", ref)
		}
	}
}

func TestEvidenceGraphFailsWhenRequiredAttemptDedicatedEvidenceIsMissing(t *testing.T) {
	store, repo := newEvidenceTestStoreWithRepo(t)
	record := validPortableAttemptSeal(t, store, repo)
	finding := putAttemptSealTestEvidence(t, store, "finding-record", "finding:graph")
	record.FindingRecordRefs = []EvidenceObjectRef{finding}
	sealRef, stored, err := store.StoreAttemptSeal(record)
	if err != nil {
		t.Fatal(err)
	}
	revision := testTaskIndexRevision(stored.SemanticTaskRef, 1, "transition-seal", time.Unix(7000, 0).UTC())
	revision.AttemptSeals = []EvidenceObjectRef{sealRef}
	taskRef, _, err := store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	headRef, ledgerRef := storeSingleTaskEvidenceAuthority(t, store, stored.SemanticTaskRef, taskRef, 1, "transition-seal", "snapshot-seal", time.Unix(7001, 0).UTC())
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.evidenceObjectPath(finding.Digest)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.validateEvidenceGraphRoots(ledgerRef, headRef, 1); err == nil {
		t.Fatal("missing required dedicated AttemptSeal evidence was accepted")
	}
}

func validPortableAttemptSeal(t *testing.T, store *Store, repo string) AttemptSeal {
	t.Helper()
	commit := controllerGitOutput(t, repo, "rev-parse", "HEAD")
	tree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	archive, roots, err := store.CaptureGitObjectArchive(repo, "attempt-portable:git", []string{commit, tree})
	if err != nil {
		t.Fatal(err)
	}
	return AttemptSeal{
		SchemaVersion:           evidenceSchemaVersion,
		RepositoryIdentity:      store.Identity().LineageID,
		SemanticTaskRef:         SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/A.md", ContractDigest: "task-a"},
		RootTaskRef:             SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root"},
		AttemptID:               "attempt-portable",
		ControllerGeneration:    1,
		SealingTransitionID:     "transition-seal",
		RevokedLeaseID:          "lease-portable",
		WorkspaceID:             "workspace-portable",
		ExecutionPurpose:        "root-work",
		SourceProjectSnapshotID: "snapshot-source",
		StartedAt:               time.Unix(6000, 0).UTC(),
		SealedAt:                time.Unix(6001, 0).UTC(),
		Disposition:             "accepted",
		ExecutionBaseOID:        commit,
		BaselineIndexTree:       tree,
		BaselineWorktreeTree:    tree,
		CurrentIndexTree:        tree,
		CurrentWorktreeTree:     tree,
		ParentAuthorityDigest:   "parent-authority",
		GitObjectArchive:        archive,
		GitObjectArchiveRoots:   roots,
		Coverage:                "complete",
	}
}

func putAttemptSealTestEvidence(t *testing.T, store *Store, kind, logicalIdentity string) EvidenceObjectRef {
	t.Helper()
	ref, err := store.PutEvidenceObject(kind, "application/json", logicalIdentity, true, []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func containsEvidenceRef(refs []EvidenceObjectRef, target EvidenceObjectRef) bool {
	for _, ref := range refs {
		if evidenceRefsEqual(ref, target) {
			return true
		}
	}
	return false
}
