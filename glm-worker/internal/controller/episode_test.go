package controller

import (
	"strings"
	"testing"
)

func TestEpisodeClosureSchedulerStaysInsideAdmittedScope(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	x := testSemanticRef("IMPLEMENTATION_TASKS/unrelated.md", "x")
	refs := map[string]SemanticTaskRef{
		b.TaskPath: b,
		c.TaskPath: c,
		x.TaskPath: x,
	}
	canonical := map[string][]string{
		b.TaskPath: {c.TaskPath},
		c.TaskPath: nil,
		x.TaskPath: nil,
	}
	closure, order, err := buildEpisodeClosure(b, refs, canonical, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure) != 2 || taskRefIn(closure, x) {
		t.Fatalf("admitted closure escaped blocker scope: %#v", closure)
	}
	revision := BlockerEpisodeRevision{
		ScopeRootTaskRef: b,
		AdmittedClosure:  closure,
		AdmittedOrder:    order,
	}
	first := scheduleEpisodeRevision(revision)
	if first.NextTaskRef == nil || !first.NextTaskRef.Equal(c) || first.Intent != FindingIntentStartBlockerTask {
		t.Fatalf("first schedule = %#v", first)
	}
	revision.SatisfiedTaskRefs = []SemanticTaskRef{c}
	second := scheduleEpisodeRevision(revision)
	if second.NextTaskRef == nil || !second.NextTaskRef.Equal(b) {
		t.Fatalf("second schedule = %#v", second)
	}
}

func TestEpisodeSchedulerResumesPreviouslyExecutedBlocker(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	revision := BlockerEpisodeRevision{
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b, c},
		AdmittedOrder:     []SemanticTaskRef{c, b},
		SatisfiedTaskRefs: []SemanticTaskRef{c},
		ExecutionHistory:  []SemanticTaskRef{b},
	}
	result := scheduleEpisodeRevision(revision)
	if result.NextTaskRef == nil || !result.NextTaskRef.Equal(b) || result.Intent != FindingIntentResumeBlockerTask {
		t.Fatalf("resume schedule = %#v", result)
	}
}

func TestEpisodeCycleIsRejected(t *testing.T) {
	a := testSemanticRef("IMPLEMENTATION_TASKS/a.md", "a")
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	edges := []EpisodeDependencyEdge{
		{BlockedTaskRef: a, DependencyTaskRef: b, FindingID: "finding-a-b"},
		{BlockedTaskRef: b, DependencyTaskRef: a, FindingID: "finding-b-a"},
	}
	if err := validateEpisodeAcyclic(map[string][]string{}, edges, nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle was not rejected: %v", err)
	}
}

func TestEpisodeSatisfiedScopeRequestsRootResume(t *testing.T) {
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	revision := BlockerEpisodeRevision{
		ScopeRootTaskRef:  b,
		AdmittedClosure:   []SemanticTaskRef{b},
		AdmittedOrder:     []SemanticTaskRef{b},
		SatisfiedTaskRefs: []SemanticTaskRef{b},
	}
	result := scheduleEpisodeRevision(revision)
	if result.Intent != FindingIntentResumeRootTask || result.NextTaskRef != nil {
		t.Fatalf("root resume schedule = %#v", result)
	}
}

func TestEpisodeNoRunnableDoesNotFallThroughOutsideClosure(t *testing.T) {
	a := testSemanticRef("IMPLEMENTATION_TASKS/a.md", "a")
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	revision := BlockerEpisodeRevision{
		ScopeRootTaskRef: a,
		AdmittedClosure:  []SemanticTaskRef{a, b},
		AdmittedOrder:    []SemanticTaskRef{a},
		DependencyEdges: []EpisodeDependencyEdge{
			{BlockedTaskRef: a, DependencyTaskRef: b, FindingID: "finding-a-b"},
		},
	}
	result := scheduleEpisodeRevision(revision)
	if result.Intent != FindingIntentNoRunnable || result.NextTaskRef != nil || result.Reason == "" {
		t.Fatalf("no-runnable schedule = %#v", result)
	}
}

func TestEpisodeReplanKeepsSingleEpisodeIdentityAndScope(t *testing.T) {
	root := testSemanticRef("IMPLEMENTATION_TASKS/root.md", "root")
	b := testSemanticRef("IMPLEMENTATION_TASKS/b.md", "b")
	c := testSemanticRef("IMPLEMENTATION_TASKS/c.md", "c")
	previous := BlockerEpisodeRevision{
		EpisodeID:        "episode-1",
		Revision:         2,
		RevisionID:       "revision-2",
		RootTaskRef:      root,
		ScopeRootTaskRef: b,
		State:            EpisodeStatePlanned,
	}
	head := RepositoryControllerHead{RootTaskRef: &root, ControllerGeneration: 7}
	finding := FindingRecord{
		FindingID:                 "finding-b-c",
		SourceAttemptID:           "attempt-b",
		SourceSemanticTaskRef:     b,
		SourceWorkspaceSnapshotID: "snapshot-b",
	}
	lease := ExecutionLease{LeaseID: "lease-b", WorkspaceID: "workspace-b"}
	project := ProjectSnapshot{SnapshotID: "project-1"}
	var store Store
	revision, err := store.newBlockingRevision(finding, c, head, lease, project, &previous)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]SemanticTaskRef{b.TaskPath: b, c.TaskPath: c}
	revision, err = completeBlockingRevision(revision, finding, c, refs, map[string][]string{})
	if err != nil {
		t.Fatal(err)
	}
	if revision.EpisodeID != previous.EpisodeID || revision.Revision != 3 ||
		revision.PreviousRevisionID != previous.RevisionID || !revision.ScopeRootTaskRef.Equal(b) ||
		revision.State != EpisodeStateReplanning || len(revision.DependencyEdges) != 1 {
		t.Fatalf("replanned episode = %#v", revision)
	}
}

func testSemanticRef(path, digestSeed string) SemanticTaskRef {
	return SemanticTaskRef{TaskPath: path, ContractDigest: digestStrings("test-task", digestSeed)}
}
