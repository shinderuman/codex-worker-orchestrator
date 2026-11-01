package controller

import (
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentOnlyInstallSmokeReentryUsesSuccessorAttempt(t *testing.T) {
	t.Parallel()
	f := newParentOnlyInstallSmokeFixture(t)
	if _, err := f.fixture.store.ReenterAcceptedCandidate(f.input); err != nil {
		t.Fatal(err)
	}
	projected, err := f.fixture.store.ProjectParentCompletion()
	if err != nil || projected.Execution == nil {
		t.Fatalf("reentry did not project materialization: %#v err=%v", projected, err)
	}
	materialized, err := f.fixture.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: projected.Execution.ExpectedGeneration,
		SuspensionID: projected.Execution.SuspensionID,
	})
	if err != nil || materialized.Admission == nil {
		t.Fatalf("parent-only successor materialization: %#v err=%v", materialized, err)
	}
	successor := *materialized.Admission
	if successor.Attempt.PredecessorAttemptID != f.candidate.AttemptID {
		t.Fatal("reentry did not bind the predecessor attempt")
	}
	runtime := state.AttachStateStore(WorkflowTaskConfig(f.fixture.store.config, f.fixture.store.Identity(), successor.Attempt.SemanticTaskRef))
	taskID, err := runtime.Read("task.id")
	if err != nil || taskID != successor.Attempt.AttemptID {
		t.Fatalf("successor smoke store retained predecessor identity: %q err=%v", taskID, err)
	}
	policy := f.fixture.policy
	policy.RequireInstall = true
	accepted, err := f.fixture.store.AcceptParentCompletion(successor, CandidateAcceptanceInput{
		Message: "reentered parent-only install", Policy: policy, RequiredValidations: declaredNoValidations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	input := PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}
	_, root, release, err := f.fixture.store.CandidateInstallWorkspace(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	runID, err := qualitygate.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	f.runtime, f.runID, f.root = runtime, runID, root
	f.smoke.TaskID, f.smoke.ValidationRunID = taskID, runID
	f.smoke.Repository, f.smoke.WorkingDir = root, root
	f.smoke.Log = filepath.ToSlash(filepath.Join("install-smoke-runs", runID, "smoke.log"))
	f.writeSmoke(t)
	installed := f.installed
	installed.SourceOID = candidate.CommitOID
	result, err := f.fixture.store.RecordParentCompletionInstall(input, root, runID, installed, []byte("installer passed"))
	if err != nil {
		t.Fatalf("successor install smoke rejected: %v", err)
	}
	updated, err := f.fixture.store.LoadAcceptedCandidate(*result.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.fixture.store.requireCompletedCandidateInstall(updated); err != nil {
		t.Fatalf("successor install proof was not bound: %v", err)
	}
	next, err := f.fixture.store.ProjectParentCompletion()
	if err != nil || next.Operation != PublicationOperationPromote {
		t.Fatalf("installed successor projected redundant install: %#v err=%v", next, err)
	}
}
