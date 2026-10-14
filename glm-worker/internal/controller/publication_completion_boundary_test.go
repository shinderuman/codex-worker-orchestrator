package controller

import (
	"reflect"
	"testing"
)

func TestCompletionPublicationRefusesPublishedImplementationWithoutRewritingRefs(t *testing.T) {
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

func TestCompletionPublicationRaceAfterLocalAmendmentRetainsJournalAndFailsClosed(t *testing.T) {
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
	candidate, err = fixture.store.LoadAcceptedCandidate(*promoted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	op, err := fixture.store.planCandidatePublication(promoted.Head, candidate, *promoted.CandidateRef, candidate.BaseOID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, promoted.Head); err != nil {
		t.Fatal(err)
	}
	final, err := fixture.store.LoadAcceptedCandidate(*op.Publication.After)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.applyPlannedLocalRef(op, final); err != nil {
		t.Fatal(err)
	}
	remote := advancePublicationTestRemote(t, fixture, policy, "race.txt", "external\n")
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("partial amendment race became successful publication")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || head.Status != ControllerStatusFailClosed || head.PendingTransitionID != op.Transition.TransitionID || head.IntegrationTip != candidate.BaseOID || head.ObservedPrefix != "" {
		t.Fatal("partial amendment race discarded its journal or fabricated publication")
	}
	actual, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || actual != remote {
		t.Fatal("partial amendment race overwrote remote advancement")
	}
}
