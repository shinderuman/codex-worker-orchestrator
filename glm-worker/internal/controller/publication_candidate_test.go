package controller

import (
	"path/filepath"
	"testing"
)

func TestCandidateAcceptancePromotionPublicationAndPortableEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "accepted.txt", "accepted task result\n")
	evidence := publicationTestEvidence(t, fixture.store, source, policy)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "accept root result\n", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Head.LiveLeaseID != "" || accepted.CandidateRef == nil {
		t.Fatal("acceptance did not revoke mutation authority")
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != candidatePrepared || !candidate.EvidenceValid {
		t.Fatal("candidate was not prepared with exact evidence")
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if observed.Head.IntegrationTip != candidate.CommitOID || observed.Head.ObservedPrefix != candidate.CommitOID || observed.Head.PendingTransitionID != "" {
		t.Fatal("publication did not establish immutable canonical prefix")
	}
	project, err := fixture.store.LoadProjectSnapshot(observed.Head.ProjectSnapshotID)
	if err != nil || project.HeadOID != candidate.CommitOID {
		t.Fatal("publication project snapshot is not bound to integrated commit")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: observed.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("published candidate was rewritten")
	}
}

func TestCandidateAcceptanceRejectsMissingOrStaleEvidenceBeforeLeaseRevocation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "accepted.txt", "first state\n")
	evidence := publicationTestEvidence(t, fixture.store, source, policy)
	changedFixture := fixture
	changedFixture.source = source
	source = publicationTestEdit(t, changedFixture, "accepted.txt", "new state\n")
	for _, refs := range [][]EvidenceObjectRef{nil, evidence} {
		if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "candidate", Policy: policy, Evidence: refs}); err == nil {
			t.Fatal("incomplete or stale evidence accepted")
		}
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != source.Lease.LeaseID || head.PendingTransitionID != "" {
		t.Fatal("failed preflight revoked lease")
	}
}

func newPublicationTestFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy) {
	t.Helper()
	fixture := newFindingAcceptanceFixture(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runControllerGit(t, fixture.source.Workspace.Root, "init", "--bare", remote)
	runControllerGit(t, fixture.source.Workspace.Root, "remote", "add", "origin", remote)
	runControllerGit(t, fixture.source.Workspace.Root, "push", "origin", "HEAD:refs/heads/main")
	policy := PublicationPolicy{Remote: "origin", RemoteRef: "refs/heads/main", LocalRef: controllerGitOutput(t, fixture.source.Workspace.Root, "symbolic-ref", "HEAD")}
	snapshot, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	fixture.source, err = fixture.store.RecordAdmittedMutation(fixture.source, "configure publication fixture", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, policy
}

func publicationTestEdit(t *testing.T, fixture findingAcceptanceFixture, path, data string) Admission {
	t.Helper()
	writeSuspensionTestFile(t, fixture.source.Workspace.Root, path, data)
	snapshot, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	source, err := fixture.store.RecordAdmittedMutation(fixture.source, "publication test edit", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func publicationTestEvidence(t *testing.T, store *Store, source Admission, policy PublicationPolicy) []EvidenceObjectRef {
	t.Helper()
	tree, err := store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	return storePublicationTestEvidence(t, store, AcceptedCandidate{AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree})
}

func storePublicationTestEvidence(t *testing.T, store *Store, c AcceptedCandidate) []EvidenceObjectRef {
	t.Helper()
	var refs []EvidenceObjectRef
	for _, kind := range []string{"review", "validation"} {
		artifact, err := store.PutEvidenceObject(kind, "text/plain", kind+":"+c.SnapshotID, true, []byte("fixture exact-source proof "+c.SnapshotID))
		if err != nil {
			t.Fatal(err)
		}
		ref, err := store.StoreCandidateEvidence(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: store.identity.LineageID, AttemptID: c.AttemptID, SnapshotID: c.SnapshotID, CandidateID: c.CandidateID, BaseOID: c.BaseOID, TreeOID: c.TreeOID, Kind: kind, Result: "pass", Artifact: artifact})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	return refs
}

func TestUnpublishedCandidateRebindInvalidatesEvidenceAndPreservesSupersededLineage(t *testing.T) {
	t.Parallel()
	for _, promote := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared", true: "local-only"}[promote], func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "candidate.txt", "task result\n")
			accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "task result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
			if err != nil {
				t.Fatal(err)
			}
			original, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if promote {
				accepted, err = fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: original.CandidateID})
				if err != nil {
					t.Fatal(err)
				}
			}
			remote := advancePublicationTestRemote(t, fixture, policy, "external.txt", "external result\n")
			rebound, err := fixture.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: original.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			current, err := fixture.store.LoadAcceptedCandidate(*rebound.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if current.EvidenceValid || len(current.Evidence) != 0 || current.CandidateID == original.CandidateID || current.BaseOID != remote {
				t.Fatal("candidate rebind reused stale evidence/identity")
			}
			if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID}); err == nil {
				t.Fatal("rebound candidate published without revalidation")
			}
			checked, err := fixture.store.RevalidateAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID}, storePublicationTestEvidence(t, fixture.store, current))
			if err != nil {
				t.Fatal(err)
			}
			promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: checked.Head.ControllerGeneration, CandidateID: current.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: current.CandidateID})
			if err != nil {
				t.Fatal(err)
			}
			if published.Head.IntegrationTip != current.CommitOID {
				t.Fatal("rebound candidate was not integrated")
			}
			for _, path := range []string{"candidate.txt", "external.txt"} {
				if _, err := runGitBinary(fixture.source.Workspace.Root, nil, "cat-file", "-e", current.CommitOID+":"+path); err != nil {
					t.Fatal(err)
				}
			}
			old, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil || old.CandidateID != original.CandidateID {
				t.Fatal("superseded candidate evidence was replaced")
			}
			if _, err := fixture.store.BuildAttemptEvidenceBundle(original.SealRef); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func advancePublicationTestRemote(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, path, data string) string {
	t.Helper()
	remote := controllerGitOutput(t, fixture.source.Workspace.Root, "remote", "get-url", policy.Remote)
	clone := filepath.Join(t.TempDir(), "external")
	runControllerGit(t, fixture.source.Workspace.Root, "clone", "--branch", "main", remote, clone)
	runControllerGit(t, clone, "config", "user.name", "External Test")
	runControllerGit(t, clone, "config", "user.email", "external@test.invalid")
	writeSuspensionTestFile(t, clone, path, data)
	runControllerGit(t, clone, "add", path)
	runControllerGit(t, clone, "commit", "-qm", "external advancement")
	runControllerGit(t, clone, "push", "origin", "HEAD:refs/heads/main")
	return controllerGitOutput(t, clone, "rev-parse", "HEAD")
}
