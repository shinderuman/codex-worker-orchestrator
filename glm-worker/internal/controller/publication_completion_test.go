package controller

import (
	"os"
	"reflect"
	"testing"
)

func TestCompletionAmendmentRejectsChangedProductionAndSubject(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	for _, change := range []func(*AcceptedCandidate){
		func(c *AcceptedCandidate) { c.AttemptID = "different-attempt" },
		func(c *AcceptedCandidate) { c.ProjectSnapshotID = "different-project" },
		func(c *AcceptedCandidate) { c.TaskRef = fixture.child },
		func(c *AcceptedCandidate) { c.Evidence = nil },
		func(c *AcceptedCandidate) { c.CommitOID = c.BaseOID },
		func(c *AcceptedCandidate) { c.State = candidatePrepared },
	} {
		changed := candidate
		change(&changed)
		if _, err := fixture.store.completionImplementation(changed); err == nil {
			t.Fatal("completion admitted changes outside exact metadata/implementation authority")
		}
	}
	index, err := newSuspensionIndex(fixture.store.identity.PrimaryRoot, candidate.CommitOID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(index) }()
	if _, err := fixture.store.planTerminalMetadataFile(candidate.CommitOID, "result.txt", index, []byte("unreviewed production\n")); err != nil {
		t.Fatal(err)
	}
	tree, err := suspensionGitText(fixture.store.identity.PrimaryRoot, index, nil, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	changed := candidate
	changed.TreeOID = tree
	changed.CommitOID, err = createAcceptedCandidateCommit(fixture.store.identity.PrimaryRoot, tree, candidate.BaseOID, candidate.Message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.completionImplementation(changed); err == nil {
		t.Fatal("production amendment inherited implementation validation")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, published.Head) {
		t.Fatal("denied amendment changed Controller authority")
	}
}
