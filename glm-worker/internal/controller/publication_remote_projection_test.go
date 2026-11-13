package controller

import (
	"errors"
	"testing"
)

func TestRemoteOnlyAdvancementRebindsEveryMutableCandidate(t *testing.T) {
	for _, state := range []string{candidatePrepared, candidatePromoted} {
		for _, valid := range []bool{false, true} {
			t.Run(state+map[bool]string{false: "/revalidation", true: "/valid"}[valid], func(t *testing.T) {
				fixture, policy := newPublicationTestFixture(t)
				source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
				accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "candidate", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
				if err != nil {
					t.Fatal(err)
				}
				candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
				if err != nil {
					t.Fatal(err)
				}
				candidate.State, candidate.EvidenceValid = state, valid
				ref, err := fixture.store.storeAcceptedCandidate(candidate)
				if err != nil {
					t.Fatal(err)
				}
				head := accepted.Head
				head.AcceptedCandidateRef = &ref
				head.ControllerGeneration++
				if err := fixture.store.writeHeadCAS(accepted.Head.ControllerGeneration, head); err != nil {
					t.Fatal(err)
				}
				tip := advancePublicationTestRemote(t, fixture, policy, "remote.txt", "external\n")
				operation, err := fixture.store.ProjectParentCompletionOperation(PublicationOperationRebind)
				if err != nil || operation.Command == nil || operation.Command.SourceOID != tip {
					t.Fatalf("remote-only rebind projection: %#v %v", operation, err)
				}
				rebound, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: operation.Command.ExpectedGeneration, CandidateID: candidate.CandidateID})
				if err != nil {
					t.Fatal(err)
				}
				next, err := fixture.store.LoadAcceptedCandidate(*rebound.CandidateRef)
				if err != nil || next.BaseOID != tip || next.EvidenceValid || len(next.Evidence) != 0 {
					t.Fatalf("rebind retained stale source/evidence: %#v %v", next, err)
				}
			})
		}
	}
}

func TestRemoteContainingCandidateProjectsAndCompletesAdoption(t *testing.T) {
	for _, descendant := range []bool{false, true} {
		t.Run(map[bool]string{false: "candidate", true: "descendant"}[descendant], func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
			acceptedCompletionRuntime(t, fixture.store, source)
			_, candidate := promoteTerminalTestSource(t, fixture, policy, source)
			runControllerGit(t, source.Workspace.Root, "push", policy.Remote, candidate.CommitOID+":"+policy.RemoteRef)
			remote := candidate.CommitOID
			if descendant {
				remote = advancePublicationTestRemote(t, fixture, policy, "external.txt", "descendant\n")
			}
			operation, err := fixture.store.ProjectParentCompletionOperation(PublicationOperationAdopt)
			if err != nil || operation.Command == nil || operation.Command.SourceOID != remote {
				t.Fatalf("containing remote projection: %#v %v", operation, err)
			}
			adopted, err := fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: operation.Command.ExpectedGeneration, Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			if adopted.Head.AcceptedCandidateRef != nil || adopted.Head.PendingTransitionID != "" || adopted.Head.IntegrationTip != candidate.CommitOID || adopted.Head.ObservedRemoteTip != remote {
				t.Fatalf("completion was not observed: %#v", adopted.Head)
			}
			if got := observeTestRemote(t, source.Workspace.Root, policy); got != remote {
				t.Fatal("adoption rewrote the containing remote")
			}
		})
	}
}

func TestObservedCompletionRecoveryNeverPushes(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, intent := range []bool{false, true} {
			name := map[bool]string{false: "base", true: "advanced"}[advanced] + map[bool]string{false: "/prepared", true: "/intent"}[intent]
			t.Run(name, func(t *testing.T) { observedCompletionAbortCase(t, advanced, intent) })
		}
	}
}

func observedCompletionAbortCase(t *testing.T, advanced, intent bool) {
	t.Helper()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "candidate.txt", "candidate\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	promoted, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	runControllerGit(t, source.Workspace.Root, "push", policy.Remote, candidate.CommitOID+":"+policy.RemoteRef)
	op, head, err := fixture.store.planTerminalMetadataWithObservation(TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	if intent {
		phase, err := fixture.store.loadTransitionState(op.Transition.TransitionID)
		if err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.prepareCandidateCompletionWrite(op, &phase); err != nil {
			t.Fatal(err)
		}
		if err := fixture.store.beginCandidateCompletionWrite(op, &phase); err != nil {
			t.Fatal(err)
		}
	}
	remote := controllerGitOutput(t, source.Workspace.Root, "remote", "get-url", policy.Remote)
	runControllerGit(t, source.Workspace.Root, "--git-dir", remote, "update-ref", policy.RemoteRef, candidate.BaseOID, candidate.CommitOID)
	tip := candidate.BaseOID
	action := PublicationOperationReenter
	if advanced {
		tip = advancePublicationTestRemote(t, fixture, policy, "raced.txt", "advanced without candidate\n")
		action = PublicationOperationRebind
	}
	var advancement *PublicationAdvancementRequired
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); !errors.As(err, &advancement) || advancement.Action != action {
		t.Fatalf("observation-only recovery did not abort into %s: %v", action, err)
	}
	if got := observeTestRemote(t, source.Workspace.Root, policy); got != tip {
		t.Fatal("observation-only recovery pushed the candidate")
	}
	aborted, err := fixture.store.LoadHead()
	if err != nil || aborted.PendingTransitionID != "" {
		t.Fatalf("observation-only abort retained pending authority: %#v %v", aborted, err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); !errors.As(err, &advancement) {
		t.Fatalf("aborted observation recovery reported success: %v", err)
	}
	projection, err := fixture.store.ProjectParentCompletionOperation(action)
	if err != nil || projection.Command == nil {
		t.Fatalf("observation abort did not admit its next operation: %#v %v", projection, err)
	}
	input := PublicationInput{ExpectedGeneration: projection.Command.ExpectedGeneration, CandidateID: projection.Command.CandidateID}
	if advanced {
		_, err = fixture.store.RebindUnpublishedCandidate(input)
	} else {
		_, err = fixture.store.ReenterAcceptedCandidate(input)
	}
	if err != nil {
		t.Fatalf("observation abort could not execute its next operation: %v", err)
	}
}
