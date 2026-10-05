package controller

import (
	"strings"
	"testing"
)

func TestPublicationGuardAllowsOrdinaryLocalFastForwardWithoutPendingPublication(t *testing.T) {
	fixture, _ := newPublicationTestFixture(t)
	input := PublicationRefGuardInput{
		OldOID: strings.Repeat("1", 40),
		NewOID: strings.Repeat("2", 40),
		Ref:    "refs/heads/main",
	}
	if err := fixture.store.GuardPublicationRefUpdate(input); err != nil {
		t.Fatalf("ordinary local fast-forward rejected: %v", err)
	}
}

func TestPublicationGuardRejectsRemotePushWithoutPendingPublication(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	input := PublicationPushGuardInput{
		RemoteName: policy.Remote,
		LocalRef:   policy.LocalRef,
		LocalOID:   strings.Repeat("2", 40),
		RemoteRef:  policy.RemoteRef,
		RemoteOID:  strings.Repeat("1", 40),
	}
	if err := fixture.store.GuardPublicationPush(input); err == nil || !strings.Contains(err.Error(), "no pending controller publication authority") {
		t.Fatalf("unowned remote push admitted: %v", err)
	}
}

func TestPublicationGuardAuthorizesOnlyExactPendingPromotionRefUpdate(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message:  "candidate",
		Policy:   policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := fixture.store.observeCandidateRemote(accepted.Head, candidate)
	if err != nil {
		t.Fatal(err)
	}
	local, err := fixture.store.candidatePromotionLocal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate.State = candidatePromoted
	op, err := fixture.store.planCandidateRevision(accepted.Head, candidate, *accepted.CandidateRef, publicationPromote)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.LocalOld = local
	op.Publication.RemoteOID = remote
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: policy.LocalRef, ExpectedOld: local, ExpectedNew: candidate.CommitOID}}
	if err := fixture.store.prepareExecutionOperation(&op, accepted.Head); err != nil {
		t.Fatal(err)
	}

	exact := PublicationRefGuardInput{OldOID: local, NewOID: candidate.CommitOID, Ref: policy.LocalRef}
	if err := fixture.store.GuardPublicationRefUpdate(exact); err != nil {
		t.Fatalf("exact controller promotion rejected: %v", err)
	}
	wrong := exact
	wrong.NewOID = strings.Repeat("f", 40)
	if wrong.NewOID == candidate.CommitOID {
		wrong.NewOID = strings.Repeat("e", 40)
	}
	if err := fixture.store.GuardPublicationRefUpdate(wrong); err == nil {
		t.Fatal("non-authorized promotion ref update admitted")
	}
	unrelated := exact
	unrelated.Ref = "refs/heads/other"
	if err := fixture.store.GuardPublicationRefUpdate(unrelated); err == nil {
		t.Fatal("unrelated branch update admitted while publication ref transition owns Git mutation")
	}
}

func TestPublicationGuardAuthorizesOnlyExactPendingRemotePublication(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message:  "candidate",
		Policy:   policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	})
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
	candidate.State = candidateObserved
	op, err := fixture.store.planCandidateRevision(promoted.Head, candidate, *promoted.CandidateRef, publicationPublish)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.RemoteOID = candidate.BaseOID
	op.Publication.NewTip = candidate.CommitOID
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceHistory, Resource: policy.Remote + ":" + policy.RemoteRef, ExpectedOld: candidate.BaseOID, ExpectedNew: candidate.CommitOID}}
	if err := fixture.store.prepareExecutionOperation(&op, promoted.Head); err != nil {
		t.Fatal(err)
	}

	exact := PublicationPushGuardInput{
		RemoteName: policy.Remote,
		LocalRef:   policy.LocalRef,
		LocalOID:   candidate.CommitOID,
		RemoteRef:  policy.RemoteRef,
		RemoteOID:  candidate.BaseOID,
	}
	if err := fixture.store.GuardPublicationPush(exact); err != nil {
		t.Fatalf("exact controller publication rejected: %v", err)
	}
	wrongRemote := exact
	wrongRemote.RemoteOID = strings.Repeat("f", 40)
	if wrongRemote.RemoteOID == candidate.BaseOID {
		wrongRemote.RemoteOID = strings.Repeat("e", 40)
	}
	if err := fixture.store.GuardPublicationPush(wrongRemote); err == nil {
		t.Fatal("stale or raced remote publication admitted")
	}
	wrongTarget := exact
	wrongTarget.RemoteRef = "refs/heads/other"
	if err := fixture.store.GuardPublicationPush(wrongTarget); err == nil {
		t.Fatal("wrong remote publication target admitted")
	}
}

func TestPublicationGuardAuthorizesTerminalMetadataPublicationEffects(t *testing.T) {
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "terminal.txt", "terminal result\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	op, head, err := fixture.store.planTerminalMetadata(TerminalTaskInput{
		ExpectedGeneration: published.Head.ControllerGeneration,
		ProjectSnapshotID:  published.Head.ProjectSnapshotID,
		CandidateID:        candidate.CandidateID,
		TaskRef:            candidate.TaskRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	if len(op.Transition.Effects) < 2 {
		t.Fatalf("terminal publication effects = %#v", op.Transition.Effects)
	}
	history := op.Transition.Effects[0]
	ref := op.Transition.Effects[1]
	if err := fixture.store.GuardPublicationPush(PublicationPushGuardInput{
		RemoteName: policy.Remote,
		LocalRef:   policy.LocalRef,
		LocalOID:   history.ExpectedNew,
		RemoteRef:  policy.RemoteRef,
		RemoteOID:  history.ExpectedOld,
	}); err != nil {
		t.Fatalf("terminal remote publication rejected: %v", err)
	}
	if err := fixture.store.GuardPublicationRefUpdate(PublicationRefGuardInput{
		OldOID: ref.ExpectedOld,
		NewOID: ref.ExpectedNew,
		Ref:    ref.Resource,
	}); err != nil {
		t.Fatalf("terminal local ref update rejected: %v", err)
	}
}
