package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParentContinuationReadsAdmittedLiveLaneWithoutGrantingPrimaryMutation(t *testing.T) {
	harness := newLifecycleHarness(t)
	cfg := controllerTestConfig(harness.repo, filepath.Join(filepath.Dir(filepath.Dir(harness.store.dir)), "sessions"))
	root := harness.edit(t, harness.source, "root-binding.txt", "preserved root\n")
	episode := harness.planBlockingEpisode(t, root, harness.blocker, "parent-continuation")
	suspended, err := harness.store.SuspendExecution(root, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	lane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := harness.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	routed, err := ParentContinuationConfig(parent)
	if err != nil || routed.RepoRoot != lane.Admission.Workspace.Root || routed.RepoHash != parent.RepoHash {
		t.Fatalf("parent continuation did not bind the canonical lane: %#v %v", routed, err)
	}
	laneInput := cfg
	laneInput.RepoRoot = lane.Admission.Workspace.Root
	laneInput.RepoHash = "path-local-key"
	laneConfig, err := ParentContinuationConfig(laneInput)
	if err != nil || laneConfig.RepoHash != routed.RepoHash || laneConfig.RepositoryLockPath != routed.RepositoryLockPath {
		t.Fatalf("lane continuation retained a path-local Runtime identity: %#v %v", laneConfig, err)
	}
	if _, _, err := WorkflowExecutionTask(parent); err == nil {
		t.Fatal("parent primary workspace acquired execution mutation authority")
	}
	if err := os.RemoveAll(lane.Admission.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := ParentContinuationConfig(parent); err == nil {
		t.Fatal("missing live execution lane was accepted")
	}
	after, err := harness.store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("parent continuation read changed controller authority: %#v %v", after, err)
	}
}

func TestWorkflowBindingFollowsExecutionTaskAcrossOneLane(t *testing.T) {
	harness := newLifecycleHarness(t)
	cfg := controllerTestConfig(harness.repo, filepath.Join(filepath.Dir(filepath.Dir(harness.store.dir)), "sessions"))
	rootConfig, err := WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	root := harness.edit(t, harness.source, "root-binding.txt", "preserved root\n")
	episode := harness.planBlockingEpisode(t, root, harness.blocker, "workflow-binding")
	suspended, err := harness.store.SuspendExecution(root, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	lane, err := harness.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoRoot = lane.Admission.Workspace.Root
	blockerConfig, err := WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rootConfig.RepoHash == blockerConfig.RepoHash {
		t.Fatal("blocker reused the suspended root workflow session")
	}
	task, err := harness.store.ExecutionTaskForWorkspace(cfg.RepoRoot)
	if err != nil || !task.Equal(harness.blocker) {
		t.Fatalf("lane workflow used Plan focus instead of execution task: %#v %v", task, err)
	}
	if _, err := harness.store.ExecutionTaskForWorkspace(harness.repo); err == nil {
		t.Fatal("old primary workspace resolved a mutating workflow task")
	}
	primaryConfig := cfg
	primaryConfig.RepoRoot = harness.repo
	sameBlockerConfig, err := WorkflowConfig(primaryConfig)
	if err != nil || sameBlockerConfig.RepoHash != blockerConfig.RepoHash {
		t.Fatalf("workflow session depends on workspace path: %s %s %v", blockerConfig.RepoHash, sameBlockerConfig.RepoHash, err)
	}
	primaryLock, err := WorkflowLockPath(primaryConfig)
	if err != nil {
		t.Fatal(err)
	}
	laneLock, err := WorkflowLockPath(cfg)
	if err != nil || laneLock != primaryLock {
		t.Fatalf("workflow exclusion is path-local: %s %s %v", primaryLock, laneLock, err)
	}
}
