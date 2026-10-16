package controller

import (
	"reflect"
	"testing"
)

func TestCompletionPublicationRefusesPublishedImplementationWithoutRewritingRefs(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "implementation", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "push", policy.Remote, candidate.CommitOID+":"+policy.RemoteRef)
	if _, err := prepareCompletionFixture(fixture.store, TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}); err == nil {
		t.Fatal("published implementation was amended during local retirement")
	}
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("published implementation was amended")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, promoted.Head) {
		t.Fatal("rejected amendment changed Controller authority")
	}
	local, _, err := readExecutionRef(fixture.store.identity.PrimaryRoot, policy.LocalRef)
	if err != nil || local != candidate.CommitOID {
		t.Fatal("rejected amendment rewrote local ref")
	}
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || remote != candidate.CommitOID {
		t.Fatal("rejected amendment rewrote remote history")
	}
}

func TestCompletionPublicationRejectsSourceMutationAfterLocalRetirement(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "implementation\n")
	retired, candidate := prepareTerminalTestSource(t, fixture, policy, source)
	writeSuspensionTestFile(t, fixture.store.identity.PrimaryRoot, "result.txt", "unreviewed source\n")
	if _, err := fixture.store.RecoverExecutionOperation(retired.TransitionID); err == nil {
		t.Fatal("source mutation after local retirement was ignored")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, retired.Head) {
		t.Fatal("rejected publication changed local retirement authority")
	}
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || remote != candidate.BaseOID {
		t.Fatal("rejected publication changed remote history")
	}
	local, _, err := readExecutionRef(fixture.store.identity.PrimaryRoot, policy.LocalRef)
	if err != nil || local != candidate.CommitOID {
		t.Fatal("rejected publication changed the finalized commit")
	}
	if _, err := fixture.store.LoadEvidenceObject(*retired.CandidateRef); err != nil {
		t.Fatal("rejected publication lost local retirement evidence")
	}
}
