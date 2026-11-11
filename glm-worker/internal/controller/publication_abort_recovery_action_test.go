package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestCompletionAbortRemoteRaceRetainsRebindAction(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
	promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
	remotePath := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "remote", "get-url", policy.Remote)
	hook := filepath.Join(remotePath, "hooks", "pre-receive")
	if err := execfile.Write(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.store.PublishAcceptedCandidate(PublicationInput{
		ExpectedGeneration: promoted.Head.ControllerGeneration,
		CandidateID:        implementation.CandidateID,
	})
	var initial *PublicationAdvancementRequired
	if !errors.As(err, &initial) || initial.Action != PublicationOperationReenter {
		t.Fatalf("definite push rejection did not preserve reentry: %v", err)
	}
	_, phase, err := fixture.store.LoadTransition(initial.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	var op ExecutionOperation
	if err := readJSON(fixture.store.executionOperationPath(initial.TransitionID), &op); err != nil {
		t.Fatal(err)
	}
	if phase.Completion == nil || phase.Completion.AbortRemoteOID != implementation.BaseOID {
		t.Fatalf("abort journal did not retain the original remote: %#v", phase.Completion)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	external := advancePublicationTestRemote(t, fixture, policy, "advanced-after-abort.txt", "external change\n")
	_, err = fixture.store.RecoverExecutionOperation(initial.TransitionID)
	var raced *PublicationAdvancementRequired
	if !errors.As(err, &raced) {
		t.Fatalf("remote race was not classified as a recoverable refusal: %v", err)
	}
	if raced.Action != "rebind-unpublished-candidate" ||
		!strings.Contains(raced.Reason, errCandidateReentryExternalAdvancement.Error()) ||
		raced.RemoteOID != implementation.BaseOID {
		t.Fatalf("remote race lost repair action, reason, or abort evidence: %#v", raced)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID != "" || head.IntegrationTip != implementation.BaseOID || head.AcceptedCandidateRef == nil {
		t.Fatalf("aborted journal changed publication authority: %#v", head)
	}
	// Missing or invalid evidence and a foreign lease cannot be converted into rebind authority.
	missing := head
	missing.AcceptedCandidateRef = nil
	action, reason := fixture.store.publicationAbortRecoveryAction(op, missing, phase)
	if action != "" || reason != "aborted completion has no accepted candidate" {
		t.Fatalf("missing candidate was treated as repairable: action=%q reason=%q", action, reason)
	}
	invalidRef := *head.AcceptedCandidateRef
	invalidRef.Digest = strings.Repeat("f", len(invalidRef.Digest))
	invalid := head
	invalid.AcceptedCandidateRef = &invalidRef
	action, reason = fixture.store.publicationAbortRecoveryAction(op, invalid, phase)
	if action != "" || reason == "" {
		t.Fatalf("invalid evidence was treated as repairable: action=%q reason=%q", action, reason)
	}
	busy := head
	busy.LiveLeaseID = "foreign-attempt-lease"
	action, reason = fixture.store.publicationAbortRecoveryAction(op, busy, phase)
	if action != "" || !strings.Contains(reason, "quiescent accepted authority") {
		t.Fatalf("foreign lease was treated as repairable: action=%q reason=%q", action, reason)
	}
	if _, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{
		ExpectedGeneration: head.ControllerGeneration,
		CandidateID:        "foreign-candidate",
	}); err == nil {
		t.Fatal("foreign candidate was admitted")
	}
	rebound, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{
		ExpectedGeneration: head.ControllerGeneration,
		CandidateID:        implementation.CandidateID,
	})
	if err != nil {
		t.Fatalf("reported rebind action was not executable: %v", err)
	}
	if rebound.Head.IntegrationTip != external || rebound.Head.ObservedRemoteTip != external || rebound.CandidateRef == nil {
		t.Fatalf("rebind did not adopt current remote/source: %#v", rebound.Head)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.EvidenceValid || candidate.State != candidatePromoted && candidate.State != candidatePrepared {
		t.Fatalf("rebound candidate retained stale validation or became accepted as completed: %#v", candidate)
	}
	if _, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{
		ExpectedGeneration: head.ControllerGeneration,
		CandidateID:        implementation.CandidateID,
	}); err == nil {
		t.Fatal("stale rebind authority admitted twice")
	}
}
