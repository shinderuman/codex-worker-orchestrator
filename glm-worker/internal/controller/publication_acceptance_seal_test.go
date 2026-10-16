package controller

import (
	"testing"
)

func TestAcceptedCandidateSealArchiveRootsIncludeAdvancedCandidateBase(t *testing.T) {
	t.Parallel()
	f, p := newPublicationTestFixture(t)
	captured, err := f.store.captureSuspensionLocked(f.source)
	if err != nil {
		t.Fatal(err)
	}
	if captured.ExecutionBaseOID != f.source.Attempt.ExecutionBaseOID {
		t.Fatalf("suspension base = %s want attempt base %s", captured.ExecutionBaseOID, f.source.Attempt.ExecutionBaseOID)
	}
	remote := amendHeldTaskMetadataWithSource(t, f.store, p)
	if remote == captured.ExecutionBaseOID {
		t.Fatal("fixture did not advance the integration tip past the execution base")
	}
	tree, err := f.store.PreviewCandidateTree(f.source, p)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := createAcceptedCandidateCommit(f.store.identity.PrimaryRoot, tree, remote, "task result on the advanced base")
	if err != nil {
		t.Fatal(err)
	}
	record, err := executionTransition(f.source.Head, publicationAccept)
	if err != nil {
		t.Fatal(err)
	}
	op := ExecutionOperation{Transition: record, Source: f.source, Suspension: &captured}
	candidate := AcceptedCandidate{SchemaVersion: controllerSchemaVersion, AttemptID: f.source.Attempt.AttemptID, TaskRef: f.source.Attempt.SemanticTaskRef, RootTaskRef: f.source.Attempt.RootTaskRef, ProjectSnapshotID: f.source.Head.ProjectSnapshotID, ControllerGeneration: record.CommittedGeneration, TransitionID: record.TransitionID, BaseOID: remote, CommitOID: commit, TreeOID: tree, SnapshotID: f.source.Snapshot.ID, Message: "task result on the advanced base", State: candidatePrepared, EvidenceValid: true, Evidence: publicationTestEvidence(t, f.store, f.source, p)}
	sealRef, err := f.store.buildAcceptedCandidateSeal(op, candidate)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := f.store.LoadAttemptSeal(sealRef)
	if err != nil {
		t.Fatal(err)
	}
	roots := make(map[GitObjectArchiveRoot]bool, len(seal.GitObjectArchiveRoots))
	for _, root := range seal.GitObjectArchiveRoots {
		roots[root] = true
	}
	for _, required := range []GitObjectArchiveRoot{
		{OID: captured.ExecutionBaseOID, Type: "commit"},
		{OID: remote, Type: "commit"},
		{OID: commit, Type: "commit"},
		{OID: tree, Type: "tree"},
	} {
		if !roots[required] {
			t.Fatalf("accepted candidate seal archive roots missing %s %s: %#v", required.Type, required.OID, seal.GitObjectArchiveRoots)
		}
	}
}
