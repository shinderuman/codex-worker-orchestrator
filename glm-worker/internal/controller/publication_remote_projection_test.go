package controller

import (
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
	remote := controllerGitOutput(t, source.Workspace.Root, "remote", "get-url", policy.Remote)
	runControllerGit(t, source.Workspace.Root, "--git-dir", remote, "update-ref", policy.RemoteRef, candidate.BaseOID, candidate.CommitOID)
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("observation-only recovery accepted a remote without the candidate")
	}
	if got := observeTestRemote(t, source.Workspace.Root, policy); got != candidate.BaseOID {
		t.Fatal("observation-only recovery pushed the candidate")
	}
}
