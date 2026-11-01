package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestTypedPublicationAbortAdvertisesExecutableStagedReentry(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	cfg, _, _ := typedCompletionWorkflowFixture(t, binary, false)
	initial := executeParentHandoff(t, cfg)
	if initial.RequiredAction == nil || *initial.RequiredAction != controllerPublicationSurface {
		t.Fatalf("initial handoff did not offer publication: %#v", initial)
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
	code, _, diagnostic := runTypedPublicationCLI(t, cfg, after.RequiredActionParameters, map[string]string{}, "--operation", "reenter")
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
