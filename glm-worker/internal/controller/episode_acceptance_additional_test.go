package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalBlockingResolutionPreservesNoRunnableEpisode(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	childPath := "IMPLEMENTATION_TASKS/blocked-child.md"
	if err := os.WriteFile(filepath.Join(repo, childPath), []byte("# blocked child\n\n## Contract\n\nblocked child\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## BLOCKED\n\n- `IMPLEMENTATION_TASKS/blocked-child.md`\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "add blocked child")

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
	admission, err := store.BootstrapExecution(authority.Task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(admission.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	target := findProjectTask(project, childPath)
	if target.Empty() {
		t.Fatal("blocked child is missing from project authority")
	}
	finding, err := store.ObserveFinding(admission, FindingObservationInput{
		Producer:   "blocked-acceptance",
		ProofClass: FindingProofUnverified,
		ProblemKey: "blocked-child-problem",
	})
	if err != nil {
		t.Fatal(err)
	}
	decision := FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &target,
		BlockingBoundary: "root cannot continue",
	}
	result, err := store.ResolveFindingWithProjectAuthority(finding.FindingID, decision)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent != FindingIntentNoRunnable || result.Episode == nil || result.NextTaskRef != nil ||
		!strings.Contains(result.Reason, "BLOCKED") {
		t.Fatalf("no-runnable blocker result = %#v", result)
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, admission.Head, after)

	replay, err := store.ResolveFindingWithProjectAuthority(finding.FindingID, decision)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Intent != FindingIntentNoRunnable || replay.Episode == nil ||
		replay.Episode.RevisionID != result.Episode.RevisionID || replay.NextTaskRef != nil {
		t.Fatalf("replayed no-runnable blocker result = %#v", replay)
	}
}

func TestDuplicateBlockerTargetReusesEpisodeEdge(t *testing.T) {
	root := testSemanticRef("IMPLEMENTATION_TASKS/root.md", "root")
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	previous := BlockerEpisodeRevision{
		EpisodeID:        "episode-duplicate-target",
		Revision:         1,
		RevisionID:       "revision-1",
		RootTaskRef:      root,
		ScopeRootTaskRef: b,
		DependencyEdges: []EpisodeDependencyEdge{
			{BlockedTaskRef: b, DependencyTaskRef: c, FindingID: "finding-original"},
		},
		AdmittedClosure: []SemanticTaskRef{b, c},
		AdmittedOrder:   []SemanticTaskRef{c, b},
		State:           EpisodeStatePlanned,
	}
	finding := FindingRecord{
		FindingID:                 "finding-duplicate-target",
		SourceAttemptID:           "attempt-b",
		SourceSemanticTaskRef:     b,
		SourceWorkspaceSnapshotID: "snapshot-b",
	}
	head := RepositoryControllerHead{RootTaskRef: &root, ControllerGeneration: 7}
	lease := ExecutionLease{LeaseID: "lease-b", WorkspaceID: "workspace-b"}
	project := ProjectSnapshot{SnapshotID: "project-1"}
	var store Store
	revision, err := store.newBlockingRevision(finding, c, head, lease, project, &previous)
	if err != nil {
		t.Fatal(err)
	}
	revision, err = completeBlockingRevision(
		revision,
		finding,
		c,
		map[string]SemanticTaskRef{b.TaskPath: b, c.TaskPath: c},
		map[string][]string{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Revision != 2 || revision.TriggerFindingID != finding.FindingID || len(revision.DependencyEdges) != 1 {
		t.Fatalf("duplicate target revision = %#v", revision)
	}
	if !revision.DependencyEdges[0].BlockedTaskRef.Equal(b) || !revision.DependencyEdges[0].DependencyTaskRef.Equal(c) {
		t.Fatalf("duplicate target changed semantic edge: %#v", revision.DependencyEdges)
	}
}
