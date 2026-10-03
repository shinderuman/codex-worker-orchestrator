package controller

import "testing"

func TestRemoteRewritePreservesObservedPublicationAndFailsClosed(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "published.txt", "published result\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "published result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
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
	published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	remote := controllerGitOutput(t, fixture.source.Workspace.Root, "remote", "get-url", policy.Remote)
	runControllerGit(t, remote, "update-ref", policy.RemoteRef, candidate.BaseOID, candidate.CommitOID)
	_, err = fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy})
	if err == nil {
		t.Fatal("rewritten remote history was admitted")
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != ControllerStatusFailClosed || head.ObservedPrefix != candidate.CommitOID || head.IntegrationTip != candidate.CommitOID || head.LiveLeaseID != "" || head.PendingTransitionID != "" {
		t.Fatalf("remote rewrite lost the observed publication or minted authority: %#v", head)
	}
	var failure FailureRecord
	if head.FailureID == "" {
		t.Fatal("remote rewrite has no durable failure identity")
	}
	if err := readJSON(fixture.store.failurePath(head.FailureID), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Observed["remote"] != candidate.BaseOID || failure.Observed["observed_prefix"] != candidate.CommitOID {
		t.Fatalf("remote rewrite lost exact failure evidence: %#v", failure)
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatalf("remote rewrite destroyed published evidence: %v", err)
	}
	if actual := controllerGitOutput(t, remote, "rev-parse", policy.RemoteRef); actual != candidate.BaseOID {
		t.Fatal("controller repaired remote history without authority")
	}
}
