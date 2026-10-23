package app

import (
	"encoding/json"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalAdvancementInterruptedRecoveryHandoff(t *testing.T) {
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	published, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(cfg.RepoRoot, published.Head.RootTaskRef.TaskPath))
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, published.Head.RootTaskRef.TaskPath, string(content)+"\n親metadataの追加要求\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", published.Head.RootTaskRef.TaskPath)
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "successor amendment")
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "config", "core.hooksPath", hooks)
	_, fault := store.AdoptExternalAdvancement(controller.ExternalAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy})
	runControllerActivationGit(t, cfg.RepoRoot, "config", "--unset", "core.hooksPath")
	if fault == nil {
		t.Fatal("external advancement did not encounter injected interruption")
	}
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("pending synchronization authority: %v", err)
	}
	handoff := executeParentHandoff(t, cfg)
	var command controllerPublicationCommand
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface || json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command) != nil || command.Action != "recover" || command.TransitionID != head.PendingTransitionID {
		t.Fatalf("missing synchronization recovery: %#v", handoff)
	}
	if containsParentAction(handoff.AllowedActions, "start") || containsParentAction(handoff.AllowedActions, "start-milestones") {
		t.Fatal("pending synchronization admitted a new Task")
	}
	executeStagedParentAction(t, cfg, handoff.RequiredActionParameters["controller_command_json"])
	handoff = executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" {
		t.Fatalf("recovered synchronization: %#v", handoff)
	}
	_, st := retiredWorkflowRuntime(t, cfg)
	if st.Exists("task.id") {
		t.Fatal("synchronization recovery initialized successor Runtime")
	}
}
