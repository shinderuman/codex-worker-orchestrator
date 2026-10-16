package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationRejectsUnpromotedCandidate(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
	accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "result", Policy: policy, Evidence: publicationTestEvidence(t, f.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: c.CandidateID}); err == nil {
		t.Fatal("unpromoted candidate was published")
	}
	if observeTestRemote(t, f.store.identity.PrimaryRoot, policy) != c.BaseOID {
		t.Fatal("rejected publication changed remote")
	}
}

func TestPreparedCompletionKeepsOriginalAuthorityAndBlocksNextExecution(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
	promoted, implementation := promoteTerminalTestSource(t, f, policy, source)
	retired, err := prepareCompletionFixture(f.store, TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: implementation.CandidateID, TaskRef: implementation.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := f.store.LoadAcceptedCandidate(*retired.CandidateRef)
	if err != nil || completion.ImplementationRef == nil {
		t.Fatalf("completion source binding missing: %v", err)
	}

	if observeTestRemote(t, f.store.identity.PrimaryRoot, policy) != implementation.BaseOID || retired.Head.ObservedRemoteTip != implementation.BaseOID {
		t.Fatal("local retirement published the Task or fabricated remote observation")
	}
	if retired.Head.RootTaskRef == nil || !retired.Head.RootTaskRef.Equal(implementation.RootTaskRef) || retired.Head.ExecutionTaskRef == nil || retired.Head.LiveLeaseID != "" {
		t.Fatal("local retirement did not bind the successor Project")
	}
	if _, err := os.Stat(filepath.Join(f.store.identity.PrimaryRoot, implementation.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("completed Task Metadata was not retired before publication")
	}
	if task, canonical, err := WorkflowAuthorityTask(f.store.config); err != nil || !canonical || !task.Equal(f.child) {
		t.Fatalf("locally retired bootstrap authority differs: %v", err)
	}
	if _, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration}); err == nil {
		t.Fatal("successor execution started before publication")
	}
	if _, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: retired.Head.ControllerGeneration, CandidateID: completion.CandidateID}); err == nil {
		t.Fatal("retired execution was reopened")
	}
	published, err := f.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.AcceptedCandidateRef != nil || published.Head.IntegrationTip != completion.CommitOID || published.Head.ObservedRemoteTip != completion.CommitOID {
		t.Fatal("publication did not close exact pending authority")
	}
	if observeTestRemote(t, f.store.identity.PrimaryRoot, policy) != completion.CommitOID {
		t.Fatal("publication did not publish the retired commit")
	}
	if count := controllerGitOutput(t, f.store.identity.PrimaryRoot, "rev-list", "--count", implementation.BaseOID+".."+completion.CommitOID); count != "1" {
		t.Fatal("Task completion created more than one public commit")
	}
	if _, err := f.store.BuildAttemptEvidenceBundle(completion.SealRef); err != nil {
		t.Fatal(err)
	}
}
