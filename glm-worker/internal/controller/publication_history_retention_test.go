package controller

import (
	"strings"
	"testing"
)

func TestTerminalHistoryRetentionDoesNotReapplyCurrentSourcePolicy(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	published, completed := publishTerminalTestSource(t, fixture, policy, source)
	implementation, err := fixture.store.LoadAcceptedCandidate(*completed.ImplementationRef)
	if err != nil {
		t.Fatal(err)
	}
	implementation.TreeOID = implementation.ImplementationTree
	implementation.CommitOID, err = createAcceptedCandidateCommit(fixture.store.identity.PrimaryRoot, implementation.TreeOID, implementation.BaseOID, "retained pre-terminal source")
	if err != nil {
		t.Fatal(err)
	}
	implementation.GitArchive, _, err = fixture.store.CaptureGitObjectArchive(fixture.store.identity.PrimaryRoot, "historical-implementation", []string{implementation.CommitOID})
	if err != nil {
		t.Fatal(err)
	}
	implementation.CandidateID = ""
	implementation.Evidence = storePublicationTestEvidence(t, fixture.store, implementation)
	implementationRef, err := fixture.store.storeAcceptedCandidate(implementation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.LoadAcceptedCandidate(implementationRef); err != nil {
		t.Fatalf("fixture implementation evidence is not valid: %v", err)
	}
	completed.ImplementationRef = &implementationRef
	completed.Previous = &implementationRef
	completed.Evidence = implementation.Evidence
	completedRef, err := fixture.store.storeAcceptedCandidate(completed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.LoadAcceptedCandidate(completedRef); err == nil || !strings.Contains(err.Error(), "completion implementation authority differs") {
		t.Fatalf("historical PASS became current-source evidence: %v", err)
	}
	record, err := fixture.store.LoadTerminalTaskRecord(*published.Head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	record.CandidateRef = completedRef
	record.Previous = nil
	recordRef, err := fixture.store.putEvidenceJSON("terminal-task", record.TransitionID, record)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := fixture.store.terminalMetadataLineageRefs(recordRef)
	if err != nil {
		t.Fatalf("retention reapplied the current acceptance contract: %v", err)
	}
	for _, wanted := range []EvidenceObjectRef{recordRef, completedRef, implementationRef, completed.GitArchive, implementation.GitArchive} {
		found := false
		for _, ref := range refs {
			if evidenceRefsEqual(ref, wanted) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("retention lost referenced evidence %s", wanted.Digest)
		}
	}
	invalid := completedRef
	invalid.Length++
	if _, err := fixture.store.loadCandidateArchive(invalid); err == nil {
		t.Fatal("archive reader accepted corrupted object identity")
	}
	record.CandidateOID = implementation.CommitOID
	invalidRecord, err := fixture.store.putEvidenceJSON("terminal-task", record.TransitionID, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.terminalMetadataLineageRefs(invalidRecord); err == nil {
		t.Fatal("retention accepted a different terminal commit")
	}
}
