package controller

import "testing"

func TestCandidateEvidenceRequiresInstallWhenPolicyRequiresInstall(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "install-required.txt", "runtime-affecting change\n")
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	candidate := AcceptedCandidate{
		AttemptID:  source.Attempt.AttemptID,
		SnapshotID: source.Snapshot.ID,
		BaseOID:    source.Head.IntegrationTip,
		TreeOID:    tree,
		Policy:     policy,
	}
	refs := storePublicationTestEvidence(t, fixture.store, candidate)
	if err := fixture.store.validateCandidateEvidence(candidate, refs); err == nil {
		t.Fatal("candidate requiring install accepted without install evidence")
	}
	artifact, err := fixture.store.PutEvidenceObject("install", "text/plain", "install:"+candidate.SnapshotID, true, []byte("installed runtime proof "+candidate.SnapshotID))
	if err != nil {
		t.Fatal(err)
	}
	installRef, err := fixture.store.StoreCandidateEvidence(CandidateEvidence{
		SchemaVersion:      controllerSchemaVersion,
		RepositoryIdentity: fixture.store.identity.LineageID,
		AttemptID:          candidate.AttemptID,
		SnapshotID:         candidate.SnapshotID,
		BaseOID:            candidate.BaseOID,
		TreeOID:            candidate.TreeOID,
		Kind:               "install",
		Result:             "pass",
		Artifact:           artifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	refs = append(refs, installRef)
	if err := fixture.store.validateCandidateEvidence(candidate, refs); err != nil {
		t.Fatalf("candidate with exact install evidence rejected: %v", err)
	}
}
