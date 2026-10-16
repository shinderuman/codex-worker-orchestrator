package controller

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompletionReadinessBindsAcceptedRuntimeOutcome(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "reviewed implementation\n")
	promoted, implementation := promoteTerminalTestSource(t, fixture, policy, source)
	runtime := acceptedCompletionRuntime(t, fixture.store, source)
	prepared, err := prepareCompletionFixture(fixture.store, TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: implementation.CandidateID, TaskRef: implementation.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatal("readiness completed the Runtime before publication")
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := runtime.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("changed Runtime outcome: %v %v", accepted, err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(prepared.TransitionID); err == nil {
		t.Fatal("changed accepted outcome inherited frozen readiness")
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion || observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != implementation.BaseOID {
		t.Fatal("rejected readiness completed Runtime or changed remote")
	}
	_, phase, err := fixture.store.LoadTransition(prepared.TransitionID)
	if err != nil || phase.Completion == nil || phase.Completion.WriteIntent {
		t.Fatalf("changed Runtime outcome acquired write intent: %v", err)
	}
}
