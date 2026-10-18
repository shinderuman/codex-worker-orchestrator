package controller

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCandidateEvidenceJSONEnablesAcceptanceWithImmutableArtifacts(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "accepted.txt", "accepted result\n")
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	var refs []EvidenceObjectRef
	for _, kind := range []string{"review", "validation", "install"} {
		record := CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree, Kind: kind, Result: "pass"}
		artifact := json.RawMessage(`{"result":"pass","source":"independent-proof"}`)
		ref, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact)
		if err != nil || repeated != ref {
			t.Fatalf("evidence retry changed immutable identity: %v", err)
		}
		data, err := fixture.store.LoadEvidenceObject(ref)
		if err != nil {
			t.Fatal(err)
		}
		var stored CandidateEvidence
		if err := json.Unmarshal(data, &stored); err != nil {
			t.Fatal(err)
		}
		data, err = fixture.store.LoadEvidenceObject(stored.Artifact)
		if err != nil || !bytes.Equal(data, artifact) {
			t.Fatalf("artifact bytes changed: %v", err)
		}
		refs = append(refs, ref)
		for _, invalid := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`{"broken"`)} {
			if _, err := fixture.store.StoreCandidateEvidenceJSON(record, invalid); err == nil {
				t.Fatalf("invalid artifact accepted: %s", invalid)
			}
		}
		if _, err := fixture.store.StoreCandidateEvidenceJSON(stored, artifact); err == nil {
			t.Fatal("ambiguous artifact sources accepted")
		}
		record.Result = "fail"
		if _, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact); err == nil {
			t.Fatal("unsuccessful evidence accepted")
		}
	}
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "verified candidate", Policy: policy, Evidence: refs}); err != nil {
		t.Fatal(err)
	}
}
