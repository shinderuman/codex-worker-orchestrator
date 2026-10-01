package controller

import (
	"os"
	"path/filepath"
	"testing"
)

type findingAcceptanceFixture struct {
	store  *Store
	source Admission
	child  SemanticTaskRef
}

func TestFindingUnverifiedCannotBecomeSameTaskAuthority(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	before, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	finding := observeAcceptanceFinding(t, fixture, "ambiguous-same-task", "reviewer-a")

	result, err := fixture.store.ResolveFinding(finding.FindingID, FindingDecision{Kind: FindingDecisionSameTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentAwaitingDisposition || result.Disposition != nil {
		t.Fatalf("unverified finding gained same-task authority: %#v", result)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, before, after)
}

func TestFindingIndependentNonBlockingPreservesExecutionAuthority(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	before, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	finding := observeAcceptanceFinding(t, fixture, "nonblocking-child", "reviewer-a")
	target := fixture.child

	result, err := fixture.store.ResolveFinding(finding.FindingID, FindingDecision{
		Kind:          FindingDecisionIndependentNonBlocking,
		TargetTaskRef: &target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentRegisterNonBlocking || result.Disposition == nil ||
		result.Disposition.Kind != FindingDispositionIndependentNonBlocking ||
		result.Disposition.TargetTaskRef == nil || !result.Disposition.TargetTaskRef.Equal(target) {
		t.Fatalf("non-blocking disposition = %#v", result)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, before, after)
}

func TestFindingDuplicateConvergesOnCanonicalObservation(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	first := observeAcceptanceFinding(t, fixture, "duplicate-problem", "reviewer-a")
	second := observeAcceptanceFinding(t, fixture, "duplicate-problem", "reviewer-b")
	if first.FindingID == second.FindingID {
		t.Fatal("distinct observations collapsed into one finding identity")
	}
	if first.CanonicalFindingID != first.FindingID || second.CanonicalFindingID != first.FindingID {
		t.Fatalf("problem observations did not converge: first=%#v second=%#v", first, second)
	}

	result, err := fixture.store.ResolveFinding(second.FindingID, FindingDecision{Kind: FindingDecisionDuplicate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentDuplicate || result.Disposition == nil ||
		result.Disposition.Kind != FindingDispositionDuplicate ||
		result.Disposition.CanonicalFindingID != first.FindingID {
		t.Fatalf("duplicate disposition = %#v", result)
	}
}

func TestFindingBlockingPlansEpisodeWithoutSwitchingExecutionAuthority(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	before, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	finding := observeAcceptanceFinding(t, fixture, "blocking-child", "reviewer-a")
	target := fixture.child

	result, err := fixture.store.ResolveFinding(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &target,
		BlockingBoundary: "source task cannot continue until child is satisfied",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentOpenBlockerEpisode || result.Disposition == nil ||
		result.Disposition.Kind != FindingDispositionIndependentBlocking || result.Episode == nil ||
		result.NextTaskRef == nil || !result.NextTaskRef.Equal(target) {
		t.Fatalf("blocking episode plan = %#v", result)
	}
	if result.Episode.ScopeRootTaskRef.Empty() || !result.Episode.ScopeRootTaskRef.Equal(target) {
		t.Fatalf("blocking episode scope root = %#v", result.Episode.ScopeRootTaskRef)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, before, after)
}

func newFindingAcceptanceFixture(t *testing.T) findingAcceptanceFixture {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	childPath := "IMPLEMENTATION_TASKS/finding-child.md"
	if err := os.WriteFile(filepath.Join(repo, childPath), []byte("# finding child\n\n## Contract\n\nchild finding target\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/finding-child.md`\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "add finding target")

	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.BootstrapExecution(authority.Task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	child := findProjectTask(project, childPath)
	if child.Empty() {
		t.Fatal("finding target task missing from committed project snapshot")
	}
	return findingAcceptanceFixture{store: store, source: source, child: child}
}

func observeAcceptanceFinding(
	t *testing.T,
	fixture findingAcceptanceFixture,
	problemKey string,
	producer string,
) FindingRecord {
	t.Helper()
	finding, err := fixture.store.ObserveFinding(fixture.source, FindingObservationInput{
		Producer:   producer,
		ProofClass: FindingProofUnverified,
		ProblemKey: problemKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	return finding
}

func assertFindingHeadUnchanged(t *testing.T, before, after RepositoryControllerHead) {
	t.Helper()
	if after.ControllerGeneration != before.ControllerGeneration ||
		after.LiveAttemptID != before.LiveAttemptID || after.LiveLeaseID != before.LiveLeaseID ||
		after.ActiveEpisodeID != before.ActiveEpisodeID || after.ActiveEpisodeRevision != before.ActiveEpisodeRevision ||
		after.ExecutionTaskRef == nil || before.ExecutionTaskRef == nil ||
		!after.ExecutionTaskRef.Equal(*before.ExecutionTaskRef) {
		t.Fatalf("finding disposition changed controller execution authority: before=%#v after=%#v", before, after)
	}
}
