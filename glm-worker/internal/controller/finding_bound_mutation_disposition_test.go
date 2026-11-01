package controller

import "testing"

func TestBoundModelFindingSurvivesMutationAndTaskAuthoritySync(t *testing.T) {
	t.Parallel()
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, "IMPLEMENTATION_TASKS/revision-blocker.md"))
	edited := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, edited)
	materialized, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := fixture.store.BindModelCall(*materialized.Admission)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := fixture.store.ObserveFinding(bound, FindingObservationInput{
		Producer: "independent-reviewer", ProofClass: FindingProofUnverified,
		ProblemKey: "canonical-reviewer-invalid-decision-orphans-active-task-without-admitted-resume",
	})
	if err != nil {
		t.Fatal(err)
	}
	if finding.SourceControllerGeneration != bound.Head.ControllerGeneration {
		t.Fatal("finding was not observed at the bound model generation")
	}
	writeSuspensionTestFile(t, bound.Workspace.Root, "result.txt", "model-side mutation after observation\n")
	snapshot, err := CaptureWorkspaceSnapshot(bound.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := fixture.store.RecordAdmittedMutation(bound, "model:worker:bound-finding", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	synced, _ := syncRevisedTaskAuthority(t, fixture.store, mutated, "\n## Amendment\n\nbound finding authority sync\n")
	project, err := fixture.store.LoadProjectSnapshot(synced.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blocker := findProjectTask(project, "IMPLEMENTATION_TASKS/revision-blocker.md")
	if blocker.Empty() {
		t.Fatal("finding target was lost after task authority synchronization")
	}
	result, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind: FindingDecisionIndependentNonBlocking, TargetTaskRef: &blocker,
	})
	if err != nil {
		t.Fatalf("bound-generation finding could not be dispositioned after mutation and sync: %v", err)
	}
	if result.Disposition == nil || result.Disposition.ControllerGeneration != synced.Head.ControllerGeneration {
		t.Fatalf("finding disposition did not bind current authority: %#v", result.Disposition)
	}
	retained, err := fixture.store.LoadFinding(finding.FindingID)
	if err != nil || retained.SourceControllerGeneration != finding.SourceControllerGeneration {
		t.Fatalf("original finding observation changed: %#v err=%v", retained, err)
	}
}
