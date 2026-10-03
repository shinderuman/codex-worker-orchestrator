package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindingAttemptMutationProofAllowsSameTaskCorrection(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	if err := os.WriteFile(
		filepath.Join(fixture.source.Workspace.Root, "source.txt"),
		[]byte("attempt-owned change\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	changed, err := CaptureWorkspaceSnapshot(fixture.source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	advanced, err := fixture.store.RecordAdmittedMutation(
		fixture.source,
		"attempt-owned-change",
		"success",
		changed,
	)
	if err != nil {
		t.Fatal(err)
	}
	mutationID := onlyFindingMutationID(t, fixture.store)
	finding, err := fixture.store.ObserveFinding(advanced, FindingObservationInput{
		Producer:   "machine-proof",
		ProofClass: FindingProofAttemptMutation,
		ProblemKey: "same-task-regression",
		Evidence: []FindingEvidenceRef{
			{Kind: FindingEvidenceMutation, ID: mutationID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := fixture.store.ResolveFinding(finding.FindingID, FindingDecision{Kind: FindingDecisionSameTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentSameTaskCorrection || result.Disposition == nil ||
		result.Disposition.Kind != FindingDispositionSameTask || result.Disposition.TargetTaskRef == nil ||
		!result.Disposition.TargetTaskRef.Equal(advanced.Attempt.SemanticTaskRef) {
		t.Fatalf("machine-proven same-task result = %#v", result)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, advanced.Head, after)
}

func onlyFindingMutationID(t *testing.T, store *Store) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(store.dir, "mutations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].IsDir() || !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Fatalf("unexpected mutation evidence store: %#v", entries)
	}
	return strings.TrimSuffix(entries[0].Name(), ".json")
}
