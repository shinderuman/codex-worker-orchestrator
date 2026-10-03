package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionBaselineSurvivesGitPruningBeforeSuspension(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	writeSuspensionTestFile(t, repo, "baseline-only.txt", "valuable baseline\n")
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
	source, err := store.bootstrapExecution(authority.Task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, "baseline-only.txt")); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "gc", "--prune=now")
	if _, err := runGitBinary(repo, nil, "cat-file", "-e", source.Attempt.BaselineTrees.WorktreeTree); err == nil {
		t.Fatal("fixture baseline was not pruned")
	}
	if err := store.restoreExecutionBaseline(repo, source.Attempt); err != nil {
		t.Fatal(err)
	}
	data, err := runGitBinary(repo, nil, "cat-file", "blob", source.Attempt.BaselineTrees.WorktreeTree+":baseline-only.txt")
	if err != nil || string(data) != "valuable baseline\n" {
		t.Fatalf("baseline archive did not recover bytes: %q %v", data, err)
	}
}

func TestExecutionRecoveryRejectsOperationPayloadReplacement(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	op, head, err := fixture.store.planExecutionMaterialization(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	op.Workspace.Root = filepath.Join(t.TempDir(), "foreign")
	if err := writeJSONAtomic(fixture.store.executionOperationPath(op.Transition.TransitionID), op); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("replaced payload gained effect authority")
	}
	actual, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if actual.LiveLeaseID != "" || actual.PendingTransitionID != op.Transition.TransitionID {
		t.Fatal("rejected recovery changed authority")
	}
	if _, err := os.Lstat(op.Workspace.Root); !os.IsNotExist(err) {
		t.Fatal("replaced payload mutated foreign workspace")
	}
}
