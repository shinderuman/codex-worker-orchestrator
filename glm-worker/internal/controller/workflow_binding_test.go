package controller

import (
	"path/filepath"
	"testing"
)

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
