package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type episodeProgressionFixture struct {
	repo         string
	store        *Store
	episode      BlockerEpisodeRevision
	head         RepositoryControllerHead
	root         SemanticTaskRef
	b            SemanticTaskRef
	bAfterC      SemanticTaskRef
	c            SemanticTaskRef
	resultAfterC ProjectSnapshot
}

func TestEpisodeSatisfactionProgressesSerialResumeAcrossRetiredTasks(t *testing.T) {
	fixture := newEpisodeProgressionFixture(t)

	if _, err := progressEpisodeGraphForTest(fixture.store, EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration - 1,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             fixture.c,
	}); err == nil {
		t.Fatal("stale controller generation satisfied blocker task")
	}
	if _, err := progressEpisodeGraphForTest(fixture.store, EpisodeSatisfactionInput{
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
	first, err := progressEpisodeGraphForTest(fixture.store, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Intent != FindingIntentResumeBlockerTask || first.NextTaskRef == nil ||
		!first.NextTaskRef.Equal(fixture.bAfterC) {
		t.Fatalf("C satisfaction schedule = %#v", first)
	}
	if fixture.bAfterC.Equal(fixture.b) {
		t.Fatal("B semantic authority did not change after dependency fulfillment")
	}
	if first.Episode.Revision != 2 || !taskPathSatisfied(first.Episode.SatisfiedTaskRefs, fixture.c.TaskPath) {
		t.Fatalf("C satisfaction revision = %#v", first.Episode)
	}
	if current := taskRefForPath(first.Episode.AdmittedClosure, fixture.b.TaskPath); !current.Equal(fixture.bAfterC) {
		t.Fatalf("B was not rebound in episode closure: got=%#v want=%#v", current, fixture.bAfterC)
	}
	if taskRefIn(fixture.resultAfterC.Tasks, fixture.c) {
		t.Fatal("C unexpectedly remains in canonical result project snapshot")
	}

	retry, err := progressEpisodeGraphForTest(fixture.store, input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Episode.RevisionID != first.Episode.RevisionID || retry.Episode.Revision != first.Episode.Revision {
		t.Fatalf("duplicate C satisfaction did not converge: first=%#v retry=%#v", first.Episode, retry.Episode)
	}

	finalAuthority := commitProgressionResult(
		t,
		fixture.repo,
		fixture.store,
		[]string{fixture.b.TaskPath},
		map[string]string{
			fixture.root.TaskPath: progressionTaskBody("root", nil, []string{fixture.b.TaskPath}),
		},
		progressionPlan(false, false),
	)
	if finalAuthority.Task.Equal(fixture.root) {
		t.Fatal("root semantic authority did not change after blocker fulfillment")
	}
	head := fixture.head
	head.ControllerGeneration++
	head.ProjectSnapshotID = finalAuthority.ProjectSnapshotID
	head.RootTaskRef = &finalAuthority.Task
	head.ActiveEpisodeRevision = first.Episode.Revision
	if err := fixture.store.writeHeadCAS(fixture.head.ControllerGeneration, head); err != nil {
		t.Fatal(err)
	}

	second, err := progressEpisodeGraphForTest(fixture.store, EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             first.Episode.Revision,
		ExpectedControllerGeneration: head.ControllerGeneration,
		ProjectSnapshotID:            finalAuthority.ProjectSnapshotID,
		SatisfiedTaskRef:             fixture.bAfterC,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Intent != FindingIntentResumeRootTask || second.NextTaskRef != nil {
		t.Fatalf("B satisfaction schedule = %#v", second)
	}
	if second.Episode.Revision != 3 || second.Episode.State != EpisodeStateResumingRoot ||
		!taskPathSatisfied(second.Episode.SatisfiedTaskRefs, fixture.b.TaskPath) ||
		!taskPathSatisfied(second.Episode.SatisfiedTaskRefs, fixture.c.TaskPath) {
		t.Fatalf("B satisfaction revision = %#v", second.Episode)
	}
	if !second.Episode.RootTaskRef.Equal(finalAuthority.Task) {
		t.Fatalf("root semantic authority was not rebound: got=%#v want=%#v", second.Episode.RootTaskRef, finalAuthority.Task)
	}
	if taskRefForPath(finalAuthority.Snapshot.Tasks, fixture.b.TaskPath).TaskPath != "" {
		t.Fatal("B unexpectedly remains in canonical result project snapshot")
	}
}

func TestEpisodeSatisfactionRejectsTaskOutsideClosure(t *testing.T) {
	fixture := newEpisodeProgressionFixture(t)
	outside := SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/outside.md", ContractDigest: digestStrings("outside")}
	if _, err := progressEpisodeGraphForTest(fixture.store, EpisodeSatisfactionInput{
		EpisodeID:                    fixture.episode.EpisodeID,
		ExpectedRevision:             fixture.episode.Revision,
		ExpectedControllerGeneration: fixture.head.ControllerGeneration,
		ProjectSnapshotID:            fixture.head.ProjectSnapshotID,
		SatisfiedTaskRef:             outside,
	}); err == nil {
		t.Fatal("task outside admitted closure was accepted as satisfied")
	}
}

func newEpisodeProgressionFixture(t *testing.T) episodeProgressionFixture {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	rootPath := "IMPLEMENTATION_TASKS/root.md"
	bPath := "IMPLEMENTATION_TASKS/b.md"
	cPath := "IMPLEMENTATION_TASKS/c.md"
	writeProgressionTask(t, repo, rootPath, progressionTaskBody("root", []string{bPath}, nil))
	writeProgressionTask(t, repo, bPath, progressionTaskBody("B", []string{cPath}, nil))
	writeProgressionTask(t, repo, cPath, progressionTaskBody("C", nil, nil))
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(progressionPlan(true, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "add progression dependencies")

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
	b := findProjectTask(project, bPath)
	c := findProjectTask(project, cPath)
	if b.Empty() || c.Empty() {
		t.Fatal("progression tasks are missing from initial project snapshot")
	}

	episode := progressionEpisode(authority.Task, b, c, admission, project)
	if err := store.writeEpisodeRevision(episode); err != nil {
		t.Fatal(err)
	}
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

	resultAfterC := commitProgressionResult(
		t,
		repo,
		store,
		[]string{cPath},
		map[string]string{bPath: progressionTaskBody("B", nil, []string{cPath})},
		progressionPlan(true, false),
	)
	bAfterC := findProjectTask(resultAfterC.Snapshot, bPath)
	if bAfterC.Empty() {
		t.Fatal("B missing after C retirement")
	}
	resultHead := head
	resultHead.ControllerGeneration++
	resultHead.ProjectSnapshotID = resultAfterC.ProjectSnapshotID
	resultHead.RootTaskRef = &resultAfterC.Task
	if err := store.writeHeadCAS(head.ControllerGeneration, resultHead); err != nil {
		t.Fatal(err)
	}
	return episodeProgressionFixture{
		repo:         repo,
		store:        store,
		episode:      episode,
		head:         resultHead,
		root:         authority.Task,
		b:            b,
		bAfterC:      bAfterC,
		c:            c,
		resultAfterC: resultAfterC.Snapshot,
	}
}

func progressionEpisode(
	root SemanticTaskRef,
	b SemanticTaskRef,
	c SemanticTaskRef,
	admission Admission,
	project ProjectSnapshot,
) BlockerEpisodeRevision {
	episode := BlockerEpisodeRevision{
		SchemaVersion:              controllerSchemaVersion,
		EpisodeID:                  "episode-progression",
		Revision:                   1,
		ProjectSnapshotID:          project.SnapshotID,
		RootTaskRef:                root,
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
	return episode
}

func commitProgressionResult(
	t *testing.T,
	repo string,
	store *Store,
	remove []string,
	rewrites map[string]string,
	plan string,
) CommittedTaskAuthority {
	t.Helper()
	for _, taskPath := range remove {
		if err := os.Remove(filepath.Join(repo, filepath.FromSlash(taskPath))); err != nil {
			t.Fatal(err)
		}
	}
	for taskPath, content := range rewrites {
		writeProgressionTask(t, repo, taskPath, content)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", "-A")
	runControllerGit(t, repo, "commit", "-q", "-m", "advance progression metadata")
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.writeProjectSnapshot(authority.Snapshot); err != nil {
		t.Fatal(err)
	}
	return authority
}

func writeProgressionTask(t *testing.T, repo, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func progressionTaskBody(name string, dependencies, fulfilled []string) string {
	var body strings.Builder
	body.WriteString("# " + name + "\n\n## Contract\n\nprogression task " + name + "\n\n## Dependencies\n\n")
	writeProgressionDependencyList(&body, dependencies)
	if len(fulfilled) != 0 {
		body.WriteString("\n## Fulfilled dependencies\n\n")
		writeProgressionDependencyList(&body, fulfilled)
	}
	return body.String()
}

func writeProgressionDependencyList(body *strings.Builder, dependencies []string) {
	if len(dependencies) == 0 {
		body.WriteString("none\n")
		return
	}
	for _, dependency := range dependencies {
		body.WriteString("- `" + dependency + "`\n")
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

func taskRefForPath(refs []SemanticTaskRef, path string) SemanticTaskRef {
	for _, ref := range refs {
		if ref.TaskPath == path {
			return ref
		}
	}
	return SemanticTaskRef{}
}

func progressEpisodeGraphForTest(store *Store, input EpisodeSatisfactionInput) (EpisodeScheduleResult, error) {
	head, previous, project, err := store.validateEpisodeSatisfaction(input)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if taskPathSatisfied(previous.SatisfiedTaskRefs, input.SatisfiedTaskRef.TaskPath) && previous.ProjectSnapshotID == input.ProjectSnapshotID {
		return store.scheduleEpisodeAgainstProject(previous)
	}
	next, err := progressedEpisodeRevision(previous, head, project, input)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if err := store.writeEpisodeRevision(next); err != nil {
		return EpisodeScheduleResult{}, err
	}
	return store.scheduleEpisodeAgainstProject(next)
}

func TestEpisodeSatisfactionCannotForgePublication(t *testing.T) {
	fixture := newEpisodeProgressionFixture(t)
	if _, err := fixture.store.SatisfyEpisodeTask(EpisodeSatisfactionInput{EpisodeID: fixture.episode.EpisodeID, ExpectedRevision: fixture.episode.Revision, ExpectedControllerGeneration: fixture.head.ControllerGeneration, ProjectSnapshotID: fixture.head.ProjectSnapshotID, SatisfiedTaskRef: fixture.c}); err == nil {
		t.Fatal("unpublished Task fulfilled dependency")
	}
}
