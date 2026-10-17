package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rebindObservationLane(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	f, p := newPublicationTestFixture(t)
	source := publicationTestEdit(t, f, "initial.txt", "preserved implementation\n")
	initial, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "initial", Policy: p, Evidence: publicationTestEvidence(t, f.store, source, p)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.LoadAcceptedCandidate(*initial.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: initial.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	f.source = *lane.Admission
	source = publicationTestEdit(t, f, "result.txt", "complete task implementation\n")
	f.source = source
	accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "complete", Policy: p, Evidence: publicationTestEvidence(t, f.store, source, p)})
	if err != nil {
		t.Fatal(err)
	}
	c, err = f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, accepted, c
}

func TestRebindObservationReenterCleanupMaterialize(t *testing.T) {
	t.Parallel()
	for _, legacyStop := range []bool{false, true} {
		name := "advancement"
		if legacyStop {
			name = "same-base-stopped-authority"
		}
		t.Run(name, func(t *testing.T) {
			f, p, accepted, old := rebindObservationLane(t)
			remote := advancePublicationTestRemote(t, f, p, "external.txt", "other task\n")
			input := PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: old.CandidateID}
			rebound, err := f.store.RebindUnpublishedCandidate(input)
			if err != nil {
				t.Fatal(err)
			}
			if rebound.Head.IntegrationTip != remote || rebound.Head.ObservedRemoteTip != remote || rebound.Head.ObservedPrefix != accepted.Head.ObservedPrefix {
				t.Fatal("rebind observation/prefix mismatch")
			}
			if _, err = f.store.RebindUnpublishedCandidate(input); err == nil {
				t.Fatal("stale or duplicate rebind admitted")
			}
			c, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if c.State != candidatePrepared || c.EvidenceValid || len(c.Evidence) != 0 {
				t.Fatal("unpublished evidence was revived")
			}
			entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: c.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			cleaned, err := f.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, WorkspaceID: f.source.Workspace.ID, SealRef: c.SealRef})
			if err != nil {
				t.Fatal(err)
			}
			head := cleaned.Head
			if legacyStop {
				head.ObservedRemoteTip = old.BaseOID
				fixtureGeneration := head.ControllerGeneration
				head.ControllerGeneration++
				if err = f.store.writeHeadCAS(fixtureGeneration, head); err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID}); err == nil {
					t.Fatal("stale observation was ignored")
				}
				corrected, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: c.CandidateID})
				if err != nil {
					t.Fatal(err)
				}
				if corrected.Head.ControllerGeneration <= head.ControllerGeneration || corrected.Head.ObservedRemoteTip != remote || corrected.Head.IntegrationTip != remote || corrected.Head.ObservedPrefix != head.ObservedPrefix {
					t.Fatal("same-base rebind did not settle observation")
				}
				c, err = f.store.LoadAcceptedCandidate(*corrected.CandidateRef)
				if err != nil {
					t.Fatal(err)
				}
				if c.State != candidatePrepared || c.EvidenceValid || len(c.Evidence) != 0 {
					t.Fatal("same-base rebind revived evidence")
				}
				if got := controllerGitOutput(t, f.store.identity.PrimaryRoot, "show", "-s", "--format=%P", c.CommitOID); got != remote {
					t.Fatal("candidate does not have exactly one base parent")
				}
				head = corrected.Head
			}
			live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID})
			if err != nil {
				t.Fatal(err)
			}
			if live.Admission == nil || live.Admission.Attempt.PredecessorAttemptID != old.AttemptID || live.Admission.Lease.LeaseID == f.source.Lease.LeaseID {
				t.Fatal("materialization reused old execution authority")
			}
			for _, path := range []string{"initial.txt", "result.txt", "external.txt"} {
				if _, err = os.ReadFile(filepath.Join(live.Admission.Workspace.Root, path)); err != nil {
					t.Fatal(err)
				}
			}
			preserved, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil || preserved.CommitOID != old.CommitOID {
				t.Fatal("old candidate evidence was replaced")
			}
			if _, err = f.store.BuildAttemptEvidenceBundle(old.SealRef); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRebindObservationRejectsInconsistentRemoteBinding(t *testing.T) {
	t.Parallel()
	f, p, accepted, old := rebindObservationLane(t)
	remote := advancePublicationTestRemote(t, f, p, "binding-external.txt", "external\n")
	observed, err := f.store.unpublishedRebindRemote(accepted.Head, old)
	if err != nil || observed != remote {
		t.Fatalf("remote observation: %s, %v", observed, err)
	}
	c, err := f.store.reboundCandidate(old, remote)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.store.planCandidateRevision(accepted.Head, c, *accepted.CandidateRef, publicationRebind)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.RemoteOID = remote
	op.Publication.NewTip = remote
	if err = f.store.validatePublicationOperation(op); err != nil {
		t.Fatalf("consistent rebind binding was rejected: %v", err)
	}
	cases := []struct {
		name   string
		remote string
		tip    string
	}{
		{name: "missing remote observation", remote: "", tip: remote},
		{name: "remote differs from new tip", remote: old.BaseOID, tip: remote},
		{name: "adopted base differs from candidate base", remote: old.BaseOID, tip: old.BaseOID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op.Publication.RemoteOID = tc.remote
			op.Publication.NewTip = tc.tip
			err := f.store.validatePublicationOperation(op)
			if err == nil || !strings.Contains(err.Error(), "candidate rebind remote observation differs from adopted base") {
				t.Fatalf("inconsistent rebind binding was admitted: %v", err)
			}
		})
	}
}

func TestRebindObservationRemoteRaceRejectsStaleObservation(t *testing.T) {
	t.Parallel()
	f, p, accepted, old := rebindObservationLane(t)
	remote := advancePublicationTestRemote(t, f, p, "first-external.txt", "first\n")
	observed, err := f.store.unpublishedRebindRemote(accepted.Head, old)
	if err != nil || observed != remote {
		t.Fatalf("remote observation: %s, %v", observed, err)
	}
	c, err := f.store.reboundCandidate(old, remote)
	if err != nil {
		t.Fatal(err)
	}
	project, err := f.store.publicationProjectAdvancement(accepted.Head, remote)
	if err != nil {
		t.Fatal(err)
	}
	c.ProjectSnapshotID = project.SnapshotID
	op, err := f.store.planCandidateRevision(accepted.Head, c, *accepted.CandidateRef, publicationRebind)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.RemoteOID = remote
	op.Publication.NewTip = remote
	op.Publication.Project = &project
	op.Transition.ProjectSnapshotNew = project.SnapshotID
	op.Transition.Effects = []EffectExpectation{{Surface: MutationSurfaceRef, Resource: candidateRetentionRef(c.CommitOID), ExpectedNew: c.CommitOID}}
	if err = f.store.prepareExecutionOperation(&op, accepted.Head); err != nil {
		t.Fatal(err)
	}
	raced := advancePublicationTestRemote(t, f, p, "second-external.txt", "second\n")
	_, err = f.store.RecoverExecutionOperation(op.Transition.TransitionID)
	var advancement *PublicationAdvancementRequired
	if !errors.As(err, &advancement) || advancement.RemoteOID != raced {
		t.Fatalf("remote race accepted: %v", err)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.IntegrationTip != accepted.Head.IntegrationTip || head.ObservedRemoteTip != accepted.Head.ObservedRemoteTip || head.PendingTransitionID != "" {
		t.Fatal("raced observation changed authority")
	}
	if _, err = f.store.RecoverExecutionOperation(op.Transition.TransitionID); !errors.As(err, &advancement) {
		t.Fatal("aborted replay reported success")
	}
}
