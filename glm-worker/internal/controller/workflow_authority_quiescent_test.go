package controller

import (
	"reflect"
	"testing"
)

func TestWorkflowAuthorityTaskReadsSealedAcceptedController(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "sealed accepted result\n")
	_, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "sealed accepted result", Policy: policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.AcceptedCandidateRef == nil || head.PendingTransitionID != "" || head.ExecutionTaskRef == nil {
		t.Fatal("acceptance did not seal the controller into the quiescent accepted state")
	}
	task, canonical, err := WorkflowAuthorityTask(fixture.store.config)
	if err != nil || !canonical || !task.Equal(*head.ExecutionTaskRef) {
		t.Fatalf("quiescent accepted bootstrap read authority: %v %v %v", task, canonical, err)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, head) {
		t.Fatal("bootstrap read authority mutated the sealed controller head")
	}
}

func TestWorkflowAuthorityTaskReadsPreparedCompletionController(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "published terminal result\n")
	published, _ := prepareTerminalTestSource(t, fixture, policy, source)
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.LiveLeaseID != "" || head.LiveAttemptID != "" || head.AcceptedCandidateRef == nil || head.PendingTransitionID == "" || head.ExecutionTaskRef == nil {
		t.Fatal("completion preparation did not retain original Task authority")
	}
	if head.ControllerGeneration != published.Head.ControllerGeneration {
		t.Fatal("unexpected controller movement after publication")
	}
	task, canonical, err := WorkflowAuthorityTask(fixture.store.config)
	if err != nil || !canonical || !task.Equal(fixture.child) {
		t.Fatalf("terminal-pending bootstrap read authority: %v %v %v", task, canonical, err)
	}
}
