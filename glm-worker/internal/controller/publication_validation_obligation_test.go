package controller

import (
	"encoding/json"
	"errors"

	"testing"
)

func TestValidationObligationNeverClaimsSuccessfulValidation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "declared.txt", "source requiring final validation\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(CandidateValidationObligation{Required: []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}})
	if err != nil {
		t.Fatal(err)
	}
	record := CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree, Kind: candidateEvidenceKindValidationObligation, Result: candidateEvidencePass}
	if _, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact); err == nil {
		t.Fatal("an obligation was accepted as a passing validation")
	}
	record.Result = candidateEvidenceDeclared
	declaration, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact)
	if err != nil {
		t.Fatal(err)
	}
	proofs := publicationTestEvidence(t, fixture.store, source, policy)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "declared validation", Policy: policy, Evidence: []EvidenceObjectRef{proofs[0], declaration}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatalf("preparation bypassed final validation: %v", err)
	}
}

func TestValidationObligationMustMatchTheBoundRuntime(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "mismatch.txt", "source requiring final validation\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(CandidateValidationObligation{NotRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := fixture.store.StoreCandidateEvidenceJSON(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree, Kind: candidateEvidenceKindValidationObligation, Result: candidateEvidenceDeclared}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	proofs := publicationTestEvidence(t, fixture.store, source, policy)
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "mismatched obligation", Policy: policy, Evidence: []EvidenceObjectRef{proofs[0], ref}}); err == nil {
		t.Fatal("a declaration removed the Runtime validation obligation")
	}
}

func TestInstallObligationRequiresRealEvidenceBeforePromotion(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "install.txt", "runtime requiring installation\n")
	tree, err := fixture.store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := json.Marshal(CandidateInstallObligation{Required: true})
	if err != nil {
		t.Fatal(err)
	}
	record := CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree, Kind: candidateEvidenceKindInstallObligation, Result: candidateEvidencePass}
	if _, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact); err == nil {
		t.Fatal("installation declaration claimed successful installation")
	}
	record.Result = candidateEvidenceDeclared
	obligation, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact)
	if err != nil {
		t.Fatal(err)
	}
	proofs := publicationTestEvidence(t, fixture.store, source, policy)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "installation obligation", Policy: policy, Evidence: []EvidenceObjectRef{proofs[0], proofs[1], obligation}})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.requireCompletedCandidateInstall(candidate); !errors.Is(err, errCandidateInstallPending) {
		t.Fatalf("declaration bypassed installation: %v", err)
	}
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); !errors.Is(err, errCandidateInstallPending) {
		t.Fatalf("promotion bypassed installation: %v", err)
	}
	candidate.State = candidatePromoted
	if err := fixture.store.validateCandidateEvidence(candidate, candidate.Evidence); err == nil {
		t.Fatal("promoted candidate accepted an installation declaration without real evidence")
	}
}
