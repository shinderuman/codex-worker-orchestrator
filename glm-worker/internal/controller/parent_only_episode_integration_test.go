package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentOnlyRootResumesAfterRealChildPublicationAndAmendsSameAttempt(t *testing.T) {
	t.Parallel()
	repo, store, policy, initialRoot, episode := startParentOnlyBlockerEpisode(t)
	projected := publishParentOnlyBlocker(t, store, initialRoot, episode, policy)
	smoke := resumeAndAmendParentOnlyRoot(t, store, repo, projected)
	completeAmendedParentOnlyRoot(t, store, repo, policy, smoke)
}

func startParentOnlyBlockerEpisode(t *testing.T) (string, *Store, PublicationPolicy, Admission, BlockerEpisodeRevision) {
	t.Helper()
	repo := newControllerRepo(t)
	rootPath := "IMPLEMENTATION_TASKS/root.md"
	childPath := "IMPLEMENTATION_TASKS/parent-only-blocker.md"
	successorPath := "IMPLEMENTATION_TASKS/parent-only-successor.md"
	rootSource := "# root\n\n## External feasibility\n\nstatus: not-applicable\n\n## Execution authority\n\nmode: parent-only\n\n## Contract\n\nParent-owned root output.\n\n## Dependencies\n\n- `" + childPath + "`\n"
	childSource := "# blocker\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nBlocker implementation.\n\n## Dependencies\n\nnone\n"
	if err := os.WriteFile(filepath.Join(repo, rootPath), []byte(rootSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, childPath), []byte(childSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, successorPath), []byte(childSource), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "## ACTIVE\n\n- `" + rootPath + "`\n\n## NEXT\n\n- `" + childPath + "`\n- `" + successorPath + "`\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "parent-only blocker lifecycle")
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions"))
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	runControllerGit(t, repo, "init", "--bare", remote)
	runControllerGit(t, repo, "remote", "add", "origin", remote)
	runControllerGit(t, repo, "push", "-u", "origin", "HEAD:refs/heads/main")
	policy := PublicationPolicy{Remote: "origin", RemoteRef: "refs/heads/main", LocalRef: controllerGitOutput(t, repo, "symbolic-ref", "HEAD")}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: head.ControllerGeneration})
	if err != nil || started.Admission == nil {
		t.Fatalf("initial parent-only Root admission: %#v %v", started, err)
	}
	if started.Admission.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		t.Fatal("initial Root changed execution mode")
	}
	finding, err := store.ObserveFinding(*started.Admission, FindingObservationInput{
		Producer: "parent-only-root-reviewer", ProofClass: FindingProofUnverified, ProblemKey: "child-blocks-parent-only-root",
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(started.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	childRef := findProjectTask(project, childPath)
	if childRef.Empty() {
		t.Fatal("child Task is missing from the committed project")
	}
	disposition, err := store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind: FindingDecisionIndependentBlocking, TargetTaskRef: &childRef, BlockingBoundary: "child must be published before Root resume",
	})
	if err != nil || disposition.Episode == nil {
		t.Fatalf("parent-only blocker episode planning: %#v %v", disposition, err)
	}
	episode := *disposition.Episode
	return repo, store, policy, *started.Admission, episode
}

func publishParentOnlyBlocker(t *testing.T, store *Store, rootAdmission Admission, episode BlockerEpisodeRevision, policy PublicationPolicy) ParentCompletionOperation {
	t.Helper()
	suspended, err := store.SuspendExecution(rootAdmission, episode.EpisodeID, episode.Revision)
	if err != nil || suspended.Suspension == nil {
		t.Fatalf("parent-only Root suspension: %#v %v", suspended, err)
	}
	child, err := store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: suspended.Head.ControllerGeneration,
		EpisodeID:          episode.EpisodeID, EpisodeRevision: episode.Revision,
	})
	if err != nil || child.Admission == nil || child.Admission.Attempt.ExecutionAuthority == ExecutionAuthorityParentOnly {
		t.Fatalf("child materialization illegally used parent-only mode: %#v %v", child, err)
	}
	seedFinalValidationNoneObligation(t, store, *child.Admission)
	writeSuspensionTestFile(t, child.Admission.Workspace.Root, "blocker-result.txt", "implemented blocker\n")
	updated, err := CaptureWorkspaceSnapshot(child.Admission.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	childAdmission, err := store.RecordAdmittedMutation(*child.Admission, "finish blocker", "success", updated)
	if err != nil {
		t.Fatal(err)
	}
	evidence := publicationTestEvidence(t, store, childAdmission, policy)
	accepted, err := store.AcceptExecutionCandidate(childAdmission, CandidateAcceptanceInput{
		RequiredValidations: declaredNoValidations(), Message: "child finished", Policy: policy, Evidence: evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	childCandidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: childCandidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	retired, childCandidate := prepareCompletionTestCandidate(t, store, promoted, childCandidate)
	published, err := store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.ActiveEpisodeID != episode.EpisodeID || published.Head.ActiveEpisodeRevision <= episode.Revision {
		t.Fatalf("child publication did not progress the original blocker episode: %#v", published.Head)
	}
	cleaned, err := store.CleanupExecution(CleanupExecutionInput{
		ExpectedGeneration: published.Head.ControllerGeneration, WorkspaceID: childAdmission.Workspace.ID, SealRef: childCandidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := store.ProjectParentCompletion()
	if err != nil || projected.Operation != PublicationOperationParentOnlyMaterialize ||
		projected.Execution == nil || projected.Execution.EpisodeID != episode.EpisodeID ||
		projected.Execution.EpisodeRevision != cleaned.Head.ActiveEpisodeRevision {
		t.Fatalf("completed child did not project the parent-only Root action: %#v %v", projected, err)
	}
	return projected
}

func resumeAndAmendParentOnlyRoot(t *testing.T, store *Store, repo string, projected ParentCompletionOperation) *state.StateStore {
	t.Helper()
	rootPath := "IMPLEMENTATION_TASKS/root.md"
	resumed, err := store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{
		ExpectedGeneration: projected.Execution.ExpectedGeneration,
		EpisodeID:          projected.Execution.EpisodeID, EpisodeRevision: projected.Execution.EpisodeRevision,
	})
	if err != nil || resumed.Admission == nil {
		t.Fatalf("resuming Root parent-only materialization: %#v %v", resumed, err)
	}
	if resumed.Head.ActiveEpisodeID != "" || resumed.Admission.Attempt.SemanticTaskRef.TaskPath != rootPath ||
		resumed.Admission.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		t.Fatalf("Root continuation escaped parent-only authority: %#v", resumed)
	}
	rootAttemptID := resumed.Admission.Attempt.AttemptID
	smoke := state.AttachStateStore(WorkflowTaskConfig(store.config, store.Identity(), resumed.Admission.Attempt.SemanticTaskRef))
	if _, err := smoke.LoadControllerRuntimeBinding(); !os.IsNotExist(err) {
		t.Fatalf("Root resume fabricated model Runtime: %v", err)
	}

	rootBytes, err := os.ReadFile(filepath.Join(repo, rootPath))
	if err != nil {
		t.Fatal(err)
	}
	amended := []byte(string(rootBytes) + "\n## Amendments\n\nRetain Root across this parent-only revision.\n")
	if err := os.WriteFile(filepath.Join(repo, rootPath), amended, 0o644); err != nil {
		t.Fatal(err)
	}
	authority, err := store.ParentOnlyTaskAuthoritySyncAdmission(store.config)
	if err != nil {
		t.Fatal(err)
	}
	newRoot := SemanticTaskRef{TaskPath: rootPath, ContractDigest: digestBytes(amended)}
	proof := []byte(fmt.Sprintf(`{"runtime":"parent-only","task_id":%q}`, rootAttemptID))
	ref, err := store.PutEvidenceObject("runtime-artifact", "application/json", rootAttemptID+":"+TaskAuthoritySyncProducer+":"+authority.Snapshot.ID, true, proof)
	if err != nil {
		t.Fatal(err)
	}
	synced, err := store.SyncTaskAuthority(authority, authority.Snapshot.ID, ref, newRoot)
	if err != nil || synced.Admission == nil {
		t.Fatalf("parent-only Root Task-only source sync: %#v %v", synced, err)
	}
	if synced.Admission.Attempt.AttemptID != rootAttemptID || synced.Head.LiveAttemptID != rootAttemptID ||
		synced.Head.LiveLeaseID == resumed.Head.LiveLeaseID ||
		synced.Head.RootTaskRef == nil || !synced.Head.RootTaskRef.Equal(newRoot) {
		t.Fatalf("parent-only Amendment changed Root/attempt authority: %#v", synced)
	}
	if !store.ParentOnlySmokeIdentityMatches(rootAttemptID) {
		t.Fatal("amended Root smoke identity lost its live attempt")
	}
	amendedSmoke := state.AttachStateStore(WorkflowTaskConfig(store.config, store.Identity(), newRoot))
	if taskID, err := amendedSmoke.TaskID(); err != nil || taskID != rootAttemptID {
		t.Fatalf("amended Root state lacks its original attempt ID: %q %v", taskID, err)
	}
	if _, err := amendedSmoke.LoadControllerRuntimeBinding(); !os.IsNotExist(err) {
		t.Fatalf("amended Root state created a model Runtime binding: %v", err)
	}

	return smoke
}

func completeAmendedParentOnlyRoot(t *testing.T, store *Store, repo string, policy PublicationPolicy, smoke *state.StateStore) {
	t.Helper()
	fixture := parentOnlyTaskFixture{store: store, repo: repo, policy: policy}
	completion := parentOnlyCompletionAdmission(t, fixture)
	rootAccepted, err := store.AcceptParentCompletion(completion, CandidateAcceptanceInput{
		Message: "parent-only Root accepted", Policy: policy, RequiredValidations: declaredNoValidations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rootCandidate, err := store.LoadAcceptedCandidate(*rootAccepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promotedRoot, err := store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: rootAccepted.Head.ControllerGeneration, CandidateID: rootCandidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	promotedCandidate, err := store.LoadAcceptedCandidate(*promotedRoot.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if promotedCandidate.BundleCheckRef == nil {
		t.Fatal("parent-only Root promotion lacks its pre-publication Bundle observation")
	}
	bundleData, err := store.LoadEvidenceObject(*promotedCandidate.BundleCheckRef)
	if err != nil {
		t.Fatal(err)
	}
	var bundle CompletionBundleCheck
	if err := json.Unmarshal(bundleData, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.RuntimeTaskID != "" || bundle.TaskClockBasis != completionBundleNoModelRuntimeBasis || len(bundle.ModelCallsByRole) != 0 {
		t.Fatalf("amended parent-only Root Bundle reported unexpected model execution: %#v", bundle)
	}
	retiredRoot, _ := prepareCompletionTestCandidate(t, store, promotedRoot, promotedCandidate)
	finished, err := store.RecoverExecutionOperation(retiredRoot.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Head.MetadataLineageRef == nil || finished.Head.LiveAttemptID != "" || finished.Head.ExecutionTaskRef != nil {
		t.Fatalf("parent-only Root completion was not published and retired: %#v", finished.Head)
	}
	if _, err := smoke.LoadControllerRuntimeBinding(); !os.IsNotExist(err) {
		t.Fatalf("parent-only Root completion manufactured a model Runtime: %v", err)
	}
}
