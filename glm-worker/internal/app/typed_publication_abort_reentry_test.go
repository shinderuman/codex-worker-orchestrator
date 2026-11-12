package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestTypedPublicationAbortAdvertisesExecutableStagedReentry(t *testing.T) {
	t.Parallel()
	runIsolatedScenario(t, isolatedScenarioKeyTypedPublicationAbort, "default")
}

func TestTypedPublicationAbortRemoteRaceRebindsThroughHandoffAndStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	cfg, _, _ := typedCompletionWorkflowFixture(t, binary, false, false)
	initial := executeParentHandoff(t, cfg)
	if initial.RequiredAction == nil || *initial.RequiredAction != controllerPublicationSurface {
		t.Fatalf("initial publication handoff: %#v", initial)
	}
	remotePath := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "remote", "get-url", "origin")))
	hook := filepath.Join(remotePath, "hooks", "pre-receive")
	if err := execfile.Write(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	interruptTypedPublication(t, cfg, initial.RequiredActionParameters, false)
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.LoadHead()
	if err != nil || pending.PendingTransitionID == "" {
		t.Fatalf("interrupted publication lost its journal: %#v %v", pending, err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}

	recovery := executeParentHandoff(t, cfg)
	if recovery.RequiredAction == nil || *recovery.RequiredAction != controllerExecutionSurface {
		t.Fatalf("pending publication did not require canonical recovery: %#v", recovery)
	}
	code, output, diagnostic := runTypedParentActionCLI(t, cfg, "prepare", controllerExecutionSurface)
	if code != 0 {
		t.Fatalf("prepare recovery: %s", diagnostic)
	}
	var prepared struct {
		NextCommand []string `json:"next_command"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil || len(prepared.NextCommand) < 3 {
		t.Fatalf("prepared recovery command: %#v %v", prepared, err)
	}
	_, _, _ = runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...)
	aborted, err := store.LoadHead()
	if err != nil || aborted.PendingTransitionID != "" {
		t.Fatalf("recovery did not finalize the rejected publication journal: %#v %v", aborted, err)
	}

	external := filepath.Join(t.TempDir(), "external")
	runControllerActivationGit(t, cfg.RepoRoot, "clone", "--quiet", "--branch", "main", remotePath, external)
	runControllerActivationGit(t, external, "config", "user.name", "external")
	runControllerActivationGit(t, external, "config", "user.email", "external@example.com")
	writeAppTestFile(t, external, "remote-only-advancement.txt", "external writer\n")
	runControllerActivationGit(t, external, "add", "--all")
	runControllerActivationGit(t, external, "commit", "-qm", "remote-only update")
	runControllerActivationGit(t, external, "push", "origin", "HEAD:refs/heads/main")
	remoteOID := strings.TrimSpace(string(appTestGitOutput(t, external, "rev-parse", "HEAD")))
	if current := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"))); current == remoteOID {
		t.Fatal("test lost its remote-only race; local source was pulled")
	}

	handoff := executeParentHandoff(t, cfg)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("aborted remote race did not project a canonical publication action: %#v", handoff)
	}
	payload := handoff.RequiredActionParameters["controller_command_json"]
	var command controllerPublicationCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != controller.PublicationOperationRebind ||
		command.SourceOID != remoteOID {
		t.Fatalf("remote race did not select rebind: %#v", command)
	}
	executeStagedControllerAction(t, cfg, controllerPublicationSurface, payload, true)

	head, err := store.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil || head.IntegrationTip != remoteOID {
		t.Fatalf("staged rebind did not advance candidate source: %#v %v", head, err)
	}
	candidate, err := store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil || candidate.EvidenceValid {
		t.Fatalf("staged rebind reused stale validation: %#v %v", candidate, err)
	}
	next := executeParentHandoff(t, cfg)
	if next.RequiredAction != nil && *next.RequiredAction == controllerPublicationSurface &&
		strings.Contains(next.RequiredActionParameters["controller_command_json"], `"action":"publish"`) {
		t.Fatal("rebound candidate was published without fresh validation")
	}
}

func typedPublicationAbortBody(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	cfg, _, _ := typedCompletionWorkflowFixture(t, binary, false, false)
	initial := executeParentHandoff(t, cfg)
	if initial.RequiredAction == nil || *initial.RequiredAction != controllerPublicationSurface {
		t.Fatalf("initial handoff did not offer publication: %#v", initial)
	}
	remotePath := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "remote", "get-url", "origin")))
	refusalHook := filepath.Join(remotePath, "hooks", "pre-receive")
	if err := execfile.Write(refusalHook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	interruptTypedPublication(t, cfg, initial.RequiredActionParameters, false)
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("interrupted publication lost its journal: %v", err)
	}
	remote := strings.TrimSpace(string(appTestGitOutput(t, store.Identity().PrimaryRoot, "ls-remote", "origin", "refs/heads/main")))
	if !strings.Contains(remote, head.IntegrationTip) {
		t.Fatalf("unpublished failure changed remote: %s", remote)
	}
	if code, _, _ := runTypedParentActionCLI(t, cfg, "prepare", controllerPublicationSurface, "--operation", "reenter"); code == 0 {
		t.Fatal("pending journal admitted candidate reentry before recovery")
	}

	recovery := executeParentHandoff(t, cfg)
	if recovery.RequiredAction == nil || *recovery.RequiredAction != controllerExecutionSurface {
		t.Fatalf("journal recovery was not handed off: %#v", recovery)
	}
	var requested controller.CompletionExecutionCommand
	if err := json.Unmarshal([]byte(recovery.RequiredActionParameters["controller_command_json"]), &requested); err != nil {
		t.Fatal(err)
	}
	if requested.Action != controller.PublicationOperationRecover {
		t.Fatalf("pending journal must be recovered first, got %s", requested.Action)
	}
	code, output, diagnostic := runTypedParentActionCLI(t, cfg, "prepare", controllerExecutionSurface)
	if code != 0 {
		t.Fatalf("prepare journal recovery: %s", diagnostic)
	}
	var prepared struct {
		NextCommand []string `json:"next_command"`
	}
	if err := json.Unmarshal([]byte(output), &prepared); err != nil || len(prepared.NextCommand) < 3 {
		t.Fatalf("prepared journal recovery command: %#v %v", prepared, err)
	}
	_, _, _ = runTypedParentActionCLI(t, cfg, prepared.NextCommand[1:]...)
	if err := os.Remove(refusalHook); err != nil {
		t.Fatal(err)
	}
	head, err = store.LoadHead()
	if err != nil || head.PendingTransitionID != "" || head.AcceptedCandidateRef == nil {
		t.Fatalf("aborted publication did not restore quiescent candidate authority: %v", err)
	}
	candidate, err := store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	after := executeParentHandoff(t, cfg)
	if after.RequiredAction == nil || *after.RequiredAction != controllerPublicationSurface {
		t.Fatalf("aborted candidate lost typed parent handoff: %#v", after)
	}
	code, _, diagnostic = runTypedPublicationCLI(t, cfg, after.RequiredActionParameters, map[string]string{}, "--operation", "reenter")
	if code != 0 {
		t.Fatalf("staged reentry was advertised but not executable: %s", diagnostic)
	}
	head, err = store.LoadHead()
	if err != nil || head.AcceptedCandidateRef == nil || head.ControllerGeneration <= requested.ExpectedGeneration {
		t.Fatalf("staged reentry did not transition candidate generation: %v", err)
	}
	invalid, err := store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil || invalid.EvidenceValid || invalid.CommitOID != candidate.CommitOID {
		t.Fatalf("reentry lost source identity or reused invalid proof: %#v %v", invalid, err)
	}
	if code, _, _ := runTypedParentActionCLI(t, cfg, "prepare", controllerPublicationSurface, "--operation", "reenter"); code == 0 {
		t.Fatal("invalidated candidate incorrectly advertised another reentry")
	}
	if operation := executeParentHandoff(t, cfg); operation.RequiredAction == nil ||
		*operation.RequiredAction != controllerExecutionSurface {
		t.Fatalf("reentry did not continue through the owned execution materialization: %#v", operation)
	}
}
