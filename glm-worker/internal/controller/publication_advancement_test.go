package controller

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMutableExternalAdvancementRetainsSourceAndMintsFreshLease(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "mutable.txt", "owned work\n")
	remote := advancePublicationTestRemote(t, fixture, policy, "external.txt", "external\n")
	adopted, err := fixture.store.AdoptMutableExternalAdvancement(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Head.LiveLeaseID != "" || adopted.Head.IntegrationTip != remote || adopted.Suspension == nil {
		t.Fatal("external advancement did not revoke/seal source")
	}
	if _, err := fixture.store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
		t.Fatal("stale source retained mutation authority")
	}
	sealed, err := fixture.store.LoadAttemptSeal(*adopted.SealRef)
	if err != nil || sealed.Disposition != string(AttemptStateSuspendedForAdvancement) || sealed.EpisodeID != "" {
		t.Fatal("ordinary advancement minted blocker authority")
	}
	retained := adopted.Suspension
	seal := adopted.SealRef
	remote = advancePublicationTestRemote(t, fixture, policy, "external-second.txt", "second external advancement\n")
	adopted, err = fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: adopted.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Head.IntegrationTip != remote {
		t.Fatal("second suspended advancement was not adopted")
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: adopted.Head.ControllerGeneration, SuspensionID: retained.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || resumed.Admission.Attempt.PredecessorAttemptID != source.Attempt.AttemptID || resumed.Admission.Lease.LeaseID == source.Lease.LeaseID {
		t.Fatal("resume reused old authority")
	}
	for _, path := range []string{"mutable.txt", "external.txt", "external-second.txt"} {
		if _, err := os.ReadFile(filepath.Join(resumed.Admission.Workspace.Root, path)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(*seal); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateReentryRetiresOnlyUnobservedLocalHistory(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "accepted.txt", "owned\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "accepted result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	reentry, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	local, _, err := readExecutionRef(source.Workspace.Root, policy.LocalRef)
	if err != nil || local != c.BaseOID {
		t.Fatal("unobserved local candidate not retired")
	}
	seal, err := fixture.store.LoadAttemptSeal(c.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: reentry.Head.ControllerGeneration, SuspensionID: seal.OperationalSnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Head.AcceptedCandidateRef != nil || resumed.Admission.Attempt.AttemptID == c.AttemptID {
		t.Fatal("candidate evidence/mutation identity reused")
	}
}

func TestPublicationRaceAbortRecoveryNeverReportsSuccess(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "candidate", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	c, err = fixture.store.LoadAcceptedCandidate(*promoted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	c.State = candidateObserved
	op, err := fixture.store.planCandidateRevision(promoted.Head, c, *promoted.CandidateRef, publicationPublish)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.RemoteOID = c.BaseOID
	op.Publication.NewTip = c.CommitOID
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceHistory, Resource: policy.Remote + ":" + policy.RemoteRef, ExpectedOld: c.BaseOID, ExpectedNew: c.CommitOID}}
	if err := fixture.store.prepareExecutionOperation(&op, promoted.Head); err != nil {
		t.Fatal(err)
	}
	remote := advancePublicationTestRemote(t, fixture, policy, "raced.txt", "race\n")
	_, err = fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
	var advancement *PublicationAdvancementRequired
	if !errors.As(err, &advancement) || advancement.RemoteOID != remote {
		t.Fatalf("race classification = %v", err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); !errors.As(err, &advancement) {
		t.Fatalf("aborted retry reported success: %v", err)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID != "" || head.IntegrationTip != c.BaseOID || head.ObservedPrefix != "" {
		t.Fatal("lost race marked unpublished candidate integrated")
	}
	if _, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: c.CandidateID}); err != nil {
		t.Fatal(err)
	}
}
