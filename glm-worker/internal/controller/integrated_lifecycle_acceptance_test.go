package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type lifecycleHarness struct {
	repo      string
	store     *Store
	policy    PublicationPolicy
	source    Admission
	root      SemanticTaskRef
	blocker   SemanticTaskRef
	deeper    SemanticTaskRef
	baseLanes int
}

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
	blockerPath := "IMPLEMENTATION_TASKS/lifecycle-b.md"
	deeperPath := "IMPLEMENTATION_TASKS/lifecycle-c.md"
	rootPath := "IMPLEMENTATION_TASKS/root.md"
	writeLifecycleTask(t, repo, rootPath, lifecycleTaskBody("root", "lifecycle root", []string{blockerPath}, nil))
	writeLifecycleTask(t, repo, blockerPath, lifecycleTaskBody("blocker", "lifecycle blocker", nil, nil))
	writeLifecycleTask(t, repo, deeperPath, lifecycleTaskBody("deeper", "lifecycle deeper blocker", nil, nil))
	plan := "## ACTIVE\n\n- `" + rootPath + "`\n\n## NEXT\n\n- `" + blockerPath + "`\n- `" + deeperPath + "`\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "lifecycle corpus")

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
	harness := &lifecycleHarness{
		repo:    repo,
		store:   store,
		source:  source,
		root:    authority.Task,
		blocker: findProjectTask(project, blockerPath),
		deeper:  findProjectTask(project, deeperPath),
	}
	if harness.blocker.Empty() || harness.deeper.Empty() {
		t.Fatal("lifecycle blocker tasks missing from committed project snapshot")
	}
	harness.baseLanes = len(harness.lanes(t))
	fixture, policy := configurePublicationTestFixture(t, findingAcceptanceFixture{store: store, source: source})
	harness.source = fixture.source
	harness.policy = policy
	return harness
}

func (h *lifecycleHarness) lanes(t *testing.T) []string {
	t.Helper()
	output := controllerGitOutput(t, h.repo, "worktree", "list", "--porcelain")
	var lanes []string
	primary := ""
	for _, block := range strings.Split(strings.TrimSpace(output), "\n\n") {
		fields := strings.Split(block, "\n")
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "worktree ") {
			continue
		}
		path := strings.TrimPrefix(fields[0], "worktree ")
		if primary == "" {
			primary = path
			continue
		}
		lanes = append(lanes, path)
	}
	return lanes
}

func (h *lifecycleHarness) assertInvariant(t *testing.T, step string) {
	t.Helper()
	head, err := h.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.Status != ControllerStatusActive {
		t.Fatalf("%s: controller is fail-closed: %#v", step, head)
	}
	if head.PendingTransitionID != "" {
		t.Fatalf("%s: transition %s is still pending", step, head.PendingTransitionID)
	}
	if head.RootTaskRef == nil {
		t.Fatalf("%s: focus root authority lost", step)
	}
	live := head.LiveLeaseID != ""
	if live != (head.LiveAttemptID != "") {
		t.Fatalf("%s: partial live authority: %#v", step, head)
	}
	lanes := h.lanes(t)
	if len(lanes) > h.baseLanes+1 {
		t.Fatalf("%s: more than one derived execution lane: %v", step, lanes)
	}
	if !live {
		return
	}
	attempt, err := h.store.loadAttempt(head.LiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := h.store.loadLease(head.LiveLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.AttemptState != AttemptStateLive || attempt.AttemptID != lease.AttemptID {
		t.Fatalf("%s: live attempt/lease disagree: attempt=%#v lease=%#v", step, attempt, lease)
	}
	if lease.ControllerGeneration != head.ControllerGeneration {
		t.Fatalf("%s: live lease generation is stale: lease=%d head=%d", step, lease.ControllerGeneration, head.ControllerGeneration)
	}
	if head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(attempt.SemanticTaskRef) {
		t.Fatalf("%s: execution task disagrees with live attempt: head=%#v attempt=%#v", step, head.ExecutionTaskRef, attempt.SemanticTaskRef)
	}
	if _, err := os.Stat(h.executionWorkspaceRoot(lease)); err != nil {
		t.Fatalf("%s: live attempt workspace is missing: %v", step, err)
	}
}

func (h *lifecycleHarness) executionWorkspaceRoot(lease ExecutionLease) string {
	if lease.WorkspaceID == h.source.Lease.WorkspaceID {
		return h.source.Workspace.Root
	}
	return h.laneRoot()
}

func (h *lifecycleHarness) laneRoot() string {
	name := h.store.Identity().LineageID[:16] + "-lane"
	base, err := canonicalPath(h.store.dir)
	if err != nil {
		return ""
	}
	return filepath.Join(base, name)
}

func (h *lifecycleHarness) assertNoSecondMutatingAuthority(t *testing.T, step string) {
	t.Helper()
	workspace, err := ResolveWorkspaceIdentity(h.repo, h.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BootstrapExecution(h.root, workspace, snapshot); err == nil {
		t.Fatalf("%s: bootstrap minted authority on a non-pristine controller", step)
	}
	head, err := h.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration, EpisodeID: head.ActiveEpisodeID, EpisodeRevision: head.ActiveEpisodeRevision}); err == nil {
		t.Fatalf("%s: second mutating lane was minted", step)
	}
}

func (h *lifecycleHarness) assertQuiescentBoundaryLocked(t *testing.T, step string, episodeID string, exactRevision uint64) {
	t.Helper()
	workspace, err := ResolveWorkspaceIdentity(h.repo, h.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.BootstrapExecution(h.root, workspace, snapshot); err == nil {
		t.Fatalf("%s: bootstrap minted authority on a non-pristine controller", step)
	}
	head, err := h.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration, EpisodeID: episodeID, EpisodeRevision: exactRevision + 1}); err == nil {
		t.Fatalf("%s: stale episode revision minted a lane", step)
	}
}

func (h *lifecycleHarness) edit(t *testing.T, admission Admission, path, data string) Admission {
	t.Helper()
	writeSuspensionTestFile(t, admission.Workspace.Root, path, data)
	snapshot, err := CaptureWorkspaceSnapshot(admission.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := h.store.RecordAdmittedMutation(admission, "lifecycle edit "+path, "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func (h *lifecycleHarness) planBlockingEpisode(t *testing.T, admission Admission, target SemanticTaskRef, problemKey string) BlockerEpisodeRevision {
	t.Helper()
	finding, err := h.store.ObserveFinding(admission, FindingObservationInput{
		Producer:   "lifecycle-reviewer",
		ProofClass: FindingProofUnverified,
		ProblemKey: problemKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &target,
		BlockingBoundary: fmt.Sprintf("%s is blocked until %s is satisfied", admission.Attempt.SemanticTaskRef.TaskPath, target.TaskPath),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Episode == nil {
		t.Fatalf("blocking finding did not plan an episode: %#v", result)
	}
	return *result.Episode
}

func (h *lifecycleHarness) publishLifecycleTask(t *testing.T, admission Admission, message string) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	evidence := publicationTestEvidence(t, h.store, admission, h.policy)
	accepted, err := h.store.AcceptExecutionCandidate(admission, CandidateAcceptanceInput{Message: message, Policy: h.policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := h.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := h.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	published, err := h.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.IntegrationTip != candidate.CommitOID || published.Head.ObservedPrefix != candidate.CommitOID {
		t.Fatalf("publication did not establish canonical integration: head=%#v candidate=%s", published.Head, candidate.CommitOID)
	}
	return published, candidate
}

func (h *lifecycleHarness) retireLifecycleTask(t *testing.T, published ExecutionOperationResult, candidate AcceptedCandidate) ExecutionOperationResult {
	t.Helper()
	retired, err := h.store.RetireTerminalTask(TerminalTaskInput{
		ExpectedGeneration: published.Head.ControllerGeneration,
		ProjectSnapshotID:  published.Head.ProjectSnapshotID,
		CandidateID:        candidate.CandidateID,
		TaskRef:            candidate.TaskRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	return retired
}

func (h *lifecycleHarness) readTaskDependencies(t *testing.T, taskPath string) taskcontract.TaskDependencyState {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(h.repo, filepath.FromSlash(taskPath)))
	if err != nil {
		t.Fatal(err)
	}
	deps, err := taskcontract.ParseTaskDependencyState(content)
	if err != nil {
		t.Fatal(err)
	}
	return deps
}

func (h *lifecycleHarness) remoteTip(t *testing.T) string {
	t.Helper()
	output := controllerGitOutput(t, h.repo, "ls-remote", h.policy.Remote, h.policy.RemoteRef)
	fields := strings.SplitN(output, "\t", 2)
	if len(fields) != 2 {
		t.Fatalf("remote tip unresolvable: %q", output)
	}
	return fields[0]
}

func writeLifecycleTask(t *testing.T, repo, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lifecycleTaskBody(name, contract string, dependencies, fulfilled []string) string {
	var body strings.Builder
	body.WriteString("# " + name + "\n\n## Contract\n\n" + contract + "\n\n## Dependencies\n\n")
	if len(dependencies) == 0 {
		body.WriteString("none\n")
	} else {
		for _, dependency := range dependencies {
			body.WriteString("- `" + dependency + "`\n")
		}
	}
	if len(fulfilled) != 0 {
		body.WriteString("\n## Fulfilled dependencies\n\n")
		for _, dependency := range fulfilled {
			body.WriteString("- `" + dependency + "`\n")
		}
	}
	return body.String()
}

func TestIntegratedLifecycleNormalBlocker(t *testing.T) {
	t.Parallel()
	harness := newLifecycleHarness(t)
	rootSuspended := harness.edit(t, harness.source, "root-owned.txt", "unfinished root production work\n")

	episode := harness.planBlockingEpisode(t, rootSuspended, harness.blocker, "root-blocker")

	op, err := harness.store.planExecutionSuspension(rootSuspended, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.store.prepareExecutionOperation(&op, rootSuspended.Head); err != nil {
		t.Fatal(err)
	}
	pending, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if pending.PendingTransitionID != op.Transition.TransitionID || pending.LiveLeaseID != rootSuspended.Lease.LeaseID {
		t.Fatalf("prepared suspension did not reserve authority exactly: %#v", pending)
	}
	suspended, err := harness.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "root suspended for blocker")

	if _, err := harness.store.AdmitMutation(rootSuspended.MutationAuthority(), rootSuspended.Workspace, rootSuspended.Snapshot); err == nil {
		t.Fatal("suspended root lease regained admission")
	}
	harness.assertQuiescentBoundaryLocked(t, "quiescent blocker boundary", episode.EpisodeID, episode.Revision)

	materialized, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || !materialized.Admission.Attempt.SemanticTaskRef.Equal(harness.blocker) {
		t.Fatalf("blocker authority mismatch: %#v", materialized.Admission)
	}
	if !materialized.Admission.Attempt.RootTaskRef.Equal(harness.root) {
		t.Fatalf("blocker execution lost focus root: %#v", materialized.Admission.Attempt.RootTaskRef)
	}
	harness.source = *materialized.Admission
	harness.assertInvariant(t, "blocker materialized")
	harness.assertNoSecondMutatingAuthority(t, "blocker live")

	blocker := harness.edit(t, harness.source, "blocker-result.txt", "blocker deliverable\n")
	published, candidate := harness.publishLifecycleTask(t, blocker, "blocker result\n")
	harness.assertInvariant(t, "blocker published")
	if _, err := harness.store.AdmitMutation(blocker.MutationAuthority(), blocker.Workspace, blocker.Snapshot); err == nil {
		t.Fatal("published blocker lease regained admission")
	}

	retired := harness.retireLifecycleTask(t, published, candidate)
	harness.assertInvariant(t, "blocker terminal metadata retired")
	rootDeps := harness.readTaskDependencies(t, harness.root.TaskPath)
	if len(rootDeps.Outstanding) != 0 || len(rootDeps.Fulfilled) != 1 || rootDeps.Fulfilled[0] != harness.blocker.TaskPath {
		t.Fatalf("root dependency retirement mismatch: %#v", rootDeps)
	}

	cleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: retired.Head.ControllerGeneration,
		WorkspaceID:        blocker.Workspace.ID,
		SealRef:            candidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(blocker.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("derived lane survived cleanup: %v", err)
	}
	harness.assertInvariant(t, "blocker lane cleaned")

	resumed, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleaned.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    retired.Head.ActiveEpisodeRevision,
		SuspensionID:       suspended.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || !resumed.Admission.Attempt.SemanticTaskRef.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("root resume authority mismatch: %#v", resumed.Admission)
	}
	if resumed.Admission.Attempt.PredecessorAttemptID != rootSuspended.Attempt.AttemptID ||
		resumed.Admission.Attempt.ResumedFromSealID != suspended.SealRef.LogicalIdentity {
		t.Fatalf("root resume lost exact suspension binding: %#v", resumed.Admission.Attempt)
	}
	data, err := os.ReadFile(filepath.Join(resumed.Admission.Workspace.Root, "root-owned.txt"))
	if err != nil || string(data) != "unfinished root production work\n" {
		t.Fatalf("root-owned production work was not preserved losslessly: %q %v", data, err)
	}
	integrated, err := os.ReadFile(filepath.Join(resumed.Admission.Workspace.Root, "blocker-result.txt"))
	if err != nil || string(integrated) != "blocker deliverable\n" {
		t.Fatalf("blocker integration delta missing from rebound base: %q %v", integrated, err)
	}
	harness.source = *resumed.Admission
	harness.assertInvariant(t, "root resumed")
	if head, err := harness.store.LoadHead(); err != nil || head.ActiveEpisodeID != "" {
		t.Fatalf("resumed root left the episode open: head=%#v err=%v", head, err)
	}

	for _, seal := range []EvidenceObjectRef{*suspended.SealRef, candidate.SealRef} {
		if _, err := harness.store.BuildAttemptEvidenceBundle(seal); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegratedLifecycleBlockerOfBlockerSerialReplan(t *testing.T) {
	t.Parallel()
	runIntegratedLifecycleBlockerOfBlockerSerialReplan(t)
}

func runIntegratedLifecycleBlockerOfBlockerSerialReplan(t *testing.T) {
	harness := newLifecycleHarness(t)
	rootSuspended := harness.edit(t, harness.source, "root-owned.txt", "root work in flight\n")

	episode := harness.planBlockingEpisode(t, rootSuspended, harness.blocker, "root-blocker")
	rootSealed, err := harness.store.SuspendExecution(rootSuspended, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "root suspended")

	blockerLane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: rootSealed.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    episode.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.source = *blockerLane.Admission
	harness.assertInvariant(t, "blocker materialized")
	blocker := harness.edit(t, harness.source, "blocker-owned.txt", "unfinished blocker work\n")

	replanned := harness.planBlockingEpisode(t, blocker, harness.deeper, "blocker-blocker")
	if replanned.EpisodeID != episode.EpisodeID || replanned.Revision <= episode.Revision {
		t.Fatalf("blocker-of-blocker did not replan the same episode serially: %#v", replanned)
	}
	blockerSealed, err := harness.store.SuspendExecution(blocker, replanned.EpisodeID, replanned.Revision)
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "blocker suspended for deeper blocker")

	runControllerGit(t, harness.repo, "worktree", "lock", blocker.Workspace.Root)
	if _, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: blockerSealed.Head.ControllerGeneration,
		WorkspaceID:        blocker.Workspace.ID,
		SealRef:            *blockerSealed.SealRef,
	}); err == nil {
		t.Fatal("locked lane cleanup succeeded")
	}
	pending, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if pending.PendingTransitionID == "" || pending.LiveLeaseID != "" {
		t.Fatalf("failed cleanup discarded retry authority: %#v", pending)
	}
	if _, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: pending.ControllerGeneration,
		EpisodeID:          replanned.EpisodeID,
		EpisodeRevision:    replanned.Revision,
	}); err == nil {
		t.Fatal("pending cleanup admitted another lane")
	}
	runControllerGit(t, harness.repo, "worktree", "unlock", blocker.Workspace.Root)
	cleaned, err := harness.store.RecoverExecutionOperation(pending.PendingTransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(blocker.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("locked lane survived cleanup retry: %v", err)
	}
	harness.assertInvariant(t, "blocker lane cleaned after retry")

	deeperLane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: cleaned.Head.ControllerGeneration,
		EpisodeID:          replanned.EpisodeID,
		EpisodeRevision:    replanned.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deeperLane.Admission.Workspace.Root != blocker.Workspace.Root || deeperLane.Admission.Workspace.ID == blocker.Workspace.ID {
		t.Fatalf("same-path lane reuse did not mint a fresh workspace identity: %#v", deeperLane.Admission.Workspace)
	}
	if _, err := harness.store.AdmitMutation(blocker.MutationAuthority(), deeperLane.Admission.Workspace, deeperLane.Admission.Snapshot); err == nil {
		t.Fatal("sealed blocker lease admitted at the reused lane path")
	}
	harness.source = *deeperLane.Admission
	harness.assertInvariant(t, "deeper blocker materialized")
	harness.assertNoSecondMutatingAuthority(t, "deeper blocker live")

	deeper := harness.edit(t, harness.source, "deeper-result.txt", "deeper deliverable\n")
	deeperPublished, deeperCandidate := harness.publishLifecycleTask(t, deeper, "deeper result\n")
	deeperRetired := harness.retireLifecycleTask(t, deeperPublished, deeperCandidate)
	harness.assertInvariant(t, "deeper blocker terminal")
	episodeAfterC, err := harness.store.LoadEpisodeRevision(replanned.EpisodeID, deeperRetired.Head.ActiveEpisodeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if !taskPathSatisfied(episodeAfterC.SatisfiedTaskRefs, harness.deeper.TaskPath) {
		t.Fatalf("deeper blocker satisfaction missing from episode revision: %#v", episodeAfterC)
	}
	if taskPathSatisfied(episodeAfterC.SatisfiedTaskRefs, harness.blocker.TaskPath) {
		t.Fatalf("blocker dependency fulfilled before its own terminal transition: %#v", episodeAfterC)
	}

	deeperCleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: deeperRetired.Head.ControllerGeneration,
		WorkspaceID:        deeper.Workspace.ID,
		SealRef:            deeperCandidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "deeper blocker lane cleaned")

	blockerResumed, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: deeperCleaned.Head.ControllerGeneration,
		EpisodeID:          replanned.EpisodeID,
		EpisodeRevision:    deeperRetired.Head.ActiveEpisodeRevision,
		SuspensionID:       blockerSealed.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if blockerResumed.Admission == nil || blockerResumed.Admission.Attempt.SemanticTaskRef.TaskPath != harness.blocker.TaskPath {
		t.Fatalf("resumed blocker authority is not the scheduled blocker task: %#v", blockerResumed.Admission)
	}
	if blockerResumed.Admission.Attempt.PredecessorAttemptID != blocker.Attempt.AttemptID {
		t.Fatalf("resumed blocker lost its sealed predecessor: %#v", blockerResumed.Admission.Attempt)
	}
	resumedBlockerWork, err := os.ReadFile(filepath.Join(blockerResumed.Admission.Workspace.Root, "blocker-owned.txt"))
	if err != nil || string(resumedBlockerWork) != "unfinished blocker work\n" {
		t.Fatalf("blocker-owned work was not preserved: %q %v", resumedBlockerWork, err)
	}
	harness.source = *blockerResumed.Admission
	harness.assertInvariant(t, "blocker resumed")

	blockerFinal := harness.edit(t, harness.source, "blocker-result.txt", "blocker deliverable\n")
	blockerPublished, blockerCandidate := harness.publishLifecycleTask(t, blockerFinal, "blocker result\n")
	blockerRetired := harness.retireLifecycleTask(t, blockerPublished, blockerCandidate)
	harness.assertInvariant(t, "blocker terminal")

	blockerLaneCleaned, err := harness.store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: blockerRetired.Head.ControllerGeneration,
		WorkspaceID:        blockerFinal.Workspace.ID,
		SealRef:            blockerCandidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.assertInvariant(t, "blocker lane cleaned")

	rootResumed, err := harness.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: blockerLaneCleaned.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID,
		EpisodeRevision:    blockerRetired.Head.ActiveEpisodeRevision,
		SuspensionID:       rootSealed.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rootResumed.Admission == nil || rootResumed.Admission.Attempt.SemanticTaskRef.TaskPath != harness.root.TaskPath {
		t.Fatalf("root resume authority mismatch: %#v", rootResumed.Admission)
	}
	rootWork, err := os.ReadFile(filepath.Join(rootResumed.Admission.Workspace.Root, "root-owned.txt"))
	if err != nil || string(rootWork) != "root work in flight\n" {
		t.Fatalf("root-owned work across two blocker generations was not preserved: %q %v", rootWork, err)
	}
	for _, path := range []string{"blocker-result.txt", "deeper-result.txt"} {
		if _, err := os.Stat(filepath.Join(rootResumed.Admission.Workspace.Root, path)); err != nil {
			t.Fatalf("blocker generation delta %s missing from rebound base: %v", path, err)
		}
	}
	harness.source = *rootResumed.Admission
	harness.assertInvariant(t, "root resumed after serial replan")
	if head, err := harness.store.LoadHead(); err != nil || head.ActiveEpisodeID != "" {
		t.Fatalf("serial replan left the episode open: head=%#v err=%v", head, err)
	}

	for _, seal := range []EvidenceObjectRef{*rootSealed.SealRef, *blockerSealed.SealRef, deeperCandidate.SealRef, blockerCandidate.SealRef} {
		if _, err := harness.store.BuildAttemptEvidenceBundle(seal); err != nil {
			t.Fatal(err)
		}
	}
}
