package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
)

func TestCompletionLintFailurePreservesCandidateAndPermitsSameSourceRetry(t *testing.T) {
	cfg, store, promoted, policy, candidate := promotedWorkflowFixture(t)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	remote := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef)
	fail := filepath.Join(os.Getenv("QUALITY_TOOLS_BIN_DIR"), "fail")
	if err := os.WriteFile(fail, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err == nil || !strings.Contains(err.Error(), "completion final Metadata lint failed") {
		t.Fatalf("publication did not reject final commit lint: %v", err)
	}
	after, err := store.LoadHead()
	if err != nil || after.IntegrationTip != before.IntegrationTip || !reflect.DeepEqual(after.MetadataLineageRef, before.MetadataLineageRef) {
		t.Fatalf("lint failure retired or changed integration authority: %v", err)
	}
	if got := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); string(got) != string(remote) {
		t.Fatal("lint failure advanced the remote")
	}
	if after.AcceptedCandidateRef == nil || after.PendingTransitionID != "" {
		t.Fatal("lint abort did not preserve the accepted candidate in quiescent authority")
	}
	preserved, err := store.LoadAcceptedCandidate(*after.AcceptedCandidateRef)
	if err != nil || preserved.CandidateID != candidate.CandidateID || !preserved.EvidenceValid {
		t.Fatalf("lint abort lost reusable candidate proof: %#v %v", preserved, err)
	}
	if err := os.Remove(fail); err != nil {
		t.Fatal(err)
	}
	retried, err := store.PublishAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: after.ControllerGeneration, CandidateID: candidate.CandidateID,
	})
	if err != nil || retried.Head.MetadataLineageRef == nil ||
		retried.Head.IntegrationTip != candidate.CommitOID {
		t.Fatalf("same-source publication retry required new evidence or failed: %#v %v", retried.Head, err)
	}
	if got := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); !strings.Contains(string(got), candidate.CommitOID) {
		t.Fatalf("retry did not publish the exact accepted commit: %s", got)
	}
}

func TestCompletionInvalidPlanMetadataRejectedAndReenteredByStagedCLI(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, admission, store, policy := retiredWorkflowCorpus(t, true, "\n## 現在のGit境界\n\ninvalid duplicated state\n")
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{
		RequiredValidations: declaredNoValidations(),
		Message:             "real malformed Plan lint rejection",
		Policy:              policy,
		Evidence:            evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteBefore := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef)
	_, err = store.PublishAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
	})
	if err == nil || !strings.Contains(err.Error(), "completion final Metadata lint failed") {
		t.Fatalf("actual invalid terminal Plan was not rejected by final lint: %v", err)
	}
	after, err := store.LoadHead()
	if err != nil || after.PendingTransitionID != "" || after.AcceptedCandidateRef == nil ||
		after.IntegrationTip != promoted.Head.IntegrationTip {
		t.Fatalf("lint refusal discarded accepted candidate or changed controller: %#v %v", after, err)
	}
	if remoteAfter := appTestGitOutput(t, cfg.RepoRoot, "ls-remote", policy.Remote, policy.RemoteRef); string(remoteAfter) != string(remoteBefore) {
		t.Fatal("actual invalid Metadata updated the remote")
	}
	preserved, err := store.LoadAcceptedCandidate(*after.AcceptedCandidateRef)
	if err != nil || preserved.CandidateID != candidate.CandidateID || !preserved.EvidenceValid {
		t.Fatalf("failed lint did not retain accepted source authority: %#v %v", preserved, err)
	}
	handoff := executeParentHandoff(t, cfg)
	if handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("lint abort did not expose normal publication handoff: %#v", handoff)
	}
	code, _, diagnostic := runTypedPublicationCLI(t, cfg, handoff.RequiredActionParameters, map[string]string{}, "--operation", "reenter")
	if code != 0 {
		t.Fatalf("actual lint rejection was not repairable through staged CLI: %s", diagnostic)
	}
	reentered, err := store.LoadHead()
	if err != nil || reentered.AcceptedCandidateRef == nil || reentered.ControllerGeneration <= after.ControllerGeneration {
		t.Fatalf("staged lint recovery lost controller authority: %#v %v", reentered, err)
	}
	invalidated, err := store.LoadAcceptedCandidate(*reentered.AcceptedCandidateRef)
	if err != nil || invalidated.EvidenceValid || invalidated.CommitOID != candidate.CommitOID {
		t.Fatalf("lint reentry changed source identity or retained invalid evidence: %#v %v", invalidated, err)
	}
}
