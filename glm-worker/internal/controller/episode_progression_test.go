package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEpisodeSatisfactionProgressesSerialResumeAcrossRetiredTasks(t *testing.T) {
	fixture := newEpisodeProgressionFixture(t)

	if _, err := fixture.store.SatisfyEpisodeTask(EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration - 1,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             fixture.c,
	}); err == nil {
		t.Fatal("stale controller generation satisfied blocker task")
	}
	if _, err := fixture.store.SatisfyEpisodeTask(EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision + 1,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             fixture.c,
	}); err == nil {
		t.Fatal("stale episode revision satisfied blocker task")
	}

	input := EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             fixture.c,
	}
	first, err := fixture.store.SatisfyEpisodeTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Intent != FindingIntentResumeBlockerTask || first.NextTaskRef == nil || !first.NextTaskRef.Equal(fixture.b) {
		t.Fatalf("C satisfaction schedule = %#v", first)
	}
	if first.Episode.Revision != 2 || !taskRefIn(first.Episode.SatisfiedTaskRefs, fixture.c) {
		t.Fatalf("C satisfaction revision = %#v", first.Episode)
	}
	if taskRefIn(fixture.resultAfterC.Tasks, fixture.c) {
		t.Fatal("C unexpectedly remains in canonical result project snapshot")
	}

	retry, err := fixture.store.SatisfyEpisodeTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Episode.RevisionID != first.Episode.RevisionID || retry.Episode.Revision != first.Episode.Revision {
		t.Fatalf("duplicate C satisfaction did not converge: first=%#v retry=%#v", first.Episode, retry.Episode)
	}

	finalSnapshot := retireProgressionTask(t, fixture.repo, fixture.store, fixture.b.TaskPath, progressionPlan(false, false))
	head := fixture.head
	head.ControllerGeneration++
	head.ProjectSnapshotID = finalSnapshot.SnapshotID
	head.ActiveEpisodeRevision = first.Episode.Revision
	if err := fixture.store.writeHeadCAS(fixture.head.ControllerGeneration, head); err != nil {
		t.Fatal(err)
	}

	second, err := fixture.store.SatisfyEpisodeTask(EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             first.Episode.Revision,
		ExpectedControllerGeneration: head.ControllerGeneration,
		ProjectSnapshotID:            finalSnapshot.SnapshotID,
		SatisfiedTaskRef:             fixture.b,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Intent != FindingIntentResumeRootTask || second.NextTaskRef != nil {
		t.Fatalf("B satisfaction schedule = %#v", second)
	}
	if second.Episode.Revision != 3 || second.Episode.State != EpisodeStateResumingRoot ||
		!taskRefIn(second.Episode.SatisfiedTaskRefs, fixture.b) || !taskRefIn(second.Episode.SatisfiedTaskRefs, fixture.c) {
		t.Fatalf("B satisfaction revision = %#v", second.Episode)
	}
	if taskRefIn(finalSnapshot.Tasks, fixture.b) {
		t.Fatal("B unexpectedly remains in canonical result project snapshot")
	}
}

func TestEpisodeSatisfactionRejectsTaskOutsideClosure(t *testing.T) {
	fixture := newEpisodeProgressionFixture(t)
	outside := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/outside.md", ContractDigest: digestStrings("outside")}
	if _, err := fixture.store.SatisfyEpisodeTask(EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             outside,
	}); err == nil {
		t.Fatal("task outside admitted closure was accepted as satisfied")
	}
}

type episodeProgressionFixture struct {
	repo         string
	store        *Store
	episode      BlockerEpisodeRevision
	head         RepositoryControllerHead
	b            SemanticTaskRef
	c            SemanticTaskRef
	resultAfterC ProjectSnapshot
}

func newEpisodeProgressionFixture(t *testing.T) episodeProgressionFixture {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	writeProgressionTask(t, repo, "IMPLEMENTATION_TASKS/b.md", "B")
	writeProgressionTask(t, repo, "IMPLEMENTATION_TASKS/c.md", "C")
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(progressionPlan(true, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "add progression tasks")

	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	workspaceSnapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := store.BootstrapExecution(authority.Task, workspace, workspaceSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(admission.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	b := findProjectTask(project, "IMPLEMENTATION_TASKS/b.md")
	c := findProjectTask(project, "IMPLEMENTATION_TASKS/c.md")
	if b.Empty() || c.Empty() {
		t.Fatal("progression tasks are missing from initial project snapshot")
	}

	episode := BlockerEpisodeRevision{
		SchemaVersion:              controllerSchemaVersion,
		EpisodeID:                  "episode-progression",
		Revision:                   1,
		ProjectSnapshotID:          project.SnapshotID,
		RootTaskRef:                authority.Task,
		ScopeRootTaskRef:           b,
		DependencyEdges:            []EpisodeDependencyEdge{{BlockedTaskRef: b, DependencyTaskRef: c, FindingID: "finding-b-c"}},
		ExecutionHistory:           []SemanticTaskRef{b},
		AdmittedClosure:            []SemanticTaskRef{b, c},
		AdmittedOrder:              []SemanticTaskRef{c, b},
		SourceControllerGeneration: admission.Head.ControllerGeneration,
		SourceAttemptID:            admission.Attempt.AttemptID,
		SourceLeaseID:              admission.Lease.LeaseID,
		SourceWorkspaceID:          admission.Lease.WorkspaceID,
		SourceWorkspaceSnapshotID:  admission.Lease.ExpectedWorkspaceSnapshotID,
		State:                      EpisodeStatePlanned,
		CreatedAt:                  time.Now().UTC(),
	}
	episode.RevisionID = blockerEpisodeRevisionID(episode)
	if err := store.writeEpisodeRecord(BlockerEpisodeRecord{
		SchemaVersion:      controllerSchemaVersion,
		EpisodeID:          episode.EpisodeID,
		RepositoryIdentity: store.Identity().LineageID,
		RootTaskRef:        authority.Task,
		ScopeRootTaskRef:   b,
		OpenedByFindingID:  "finding-root-b",
		CreatedAt:          episode.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.writeEpisodeRevision(episode); err != nil {
		t.Fatal(err)
	}

	head := admission.Head
	head.ControllerGeneration++
	head.ActiveEpisodeID = episode.EpisodeID
	head.ActiveEpisodeRevision = episode.Revision
	head.ExecutionTaskRef = nil
	head.LiveAttemptID = ""
	head.LiveLeaseID = ""
	if err := store.writeHeadCAS(admission.Head.ControllerGeneration, head); err != nil {
		t.Fatal(err)
	}

	resultAfterC := retireProgressionTask(t, repo, store, c.TaskPath, progressionPlan(true, false))
	resultHead := head
	resultHead.ControllerGeneration++
	resultHead.ProjectSnapshotID = resultAfterC.SnapshotID
	if err := store.writeHeadCAS(head.ControllerGeneration, resultHead); err != nil {
		t.Fatal(err)
	}
	return episodeProgressionFixture{
		repo:         repo,
		store:        store,
		episode:      episode,
		head:         resultHead,
		b:            b,
		c:            c,
		resultAfterC: resultAfterC,
	}
}

func retireProgressionTask(
	t *testing.T,
	repo string,
	store *Store,
	taskPath string,
	plan string,
) ProjectSnapshot {
	t.Helper()
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(taskPath))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-q", "-m", "retire progression task")
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.writeProjectSnapshot(authority.Snapshot); err != nil {
		t.Fatal(err)
	}
	return authority.Snapshot
}

func writeProgressionTask(t *testing.T, repo, path, name string) {
	t.Helper()
	content := "# " + name + "\n\n## Contract\n\nprogression task " + name + "\n\n## Dependencies\n\nnone\n"
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func progressionPlan(includeB, includeC bool) string {
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n"
	if includeB || includeC {
		plan += "\n## NEXT\n"
	}
	if includeB {
		plan += "\n- `IMPLEMENTATION_TASKS/b.md`\n"
	}
	if includeC {
		plan += "\n- `IMPLEMENTATION_TASKS/c.md`\n"
	}
	return plan
}
