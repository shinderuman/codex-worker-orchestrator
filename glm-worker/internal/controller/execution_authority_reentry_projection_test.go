package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializedExecutionPredecessorAuthorityMatchesCommittedMode(t *testing.T) {
	t.Parallel()
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	admission := admitParentOnlyExecutionFixture(t, fixture)
	suspended := &SuspensionSnapshot{AttemptID: admission.Attempt.AttemptID}
	previous := &AttemptRecord{}
	lease := &ExecutionLease{}
	if err := fixture.store.bindMaterializedExecutionAuthority(task, suspended, previous, lease); err != nil {
		t.Fatalf("valid parent-only reentry refused: %v", err)
	}
	if previous.ExecutionAuthority != ExecutionAuthorityParentOnly || lease.Purpose != parentOnlyExecutionPurpose {
		t.Fatalf("parent-only predecessor authority was not inherited: attempt=%#v lease=%#v", previous, lease)
	}
	if err := os.WriteFile(filepath.Join(fixture.repo, parentOnlyEvaluationTaskPath), []byte(parentOnlyTaskContent("")), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, fixture.repo, "add", parentOnlyEvaluationTaskPath)
	runControllerGit(t, fixture.repo, "commit", "-q", "-m", "drop parent-only designation")
	runtimeAttempt := &AttemptRecord{}
	runtimeLease := &ExecutionLease{}
	if err := fixture.store.bindMaterializedExecutionAuthority(task, suspended, runtimeAttempt, runtimeLease); err == nil || !strings.Contains(err.Error(), "differs from predecessor") {
		t.Fatalf("parent-only predecessor was reentered as a Runtime: %v", err)
	}
	if runtimeAttempt.ExecutionAuthority == ExecutionAuthorityParentOnly || runtimeLease.Purpose == parentOnlyExecutionPurpose {
		t.Fatalf("rejected reentry changed attempt or lease: %#v %#v", runtimeAttempt, runtimeLease)
	}
	if err := fixture.store.bindMaterializedExecutionAuthority(task, &SuspensionSnapshot{AttemptID: "missing-predecessor"}, &AttemptRecord{}, &ExecutionLease{}); err == nil {
		t.Fatal("missing predecessor was treated as an ordinary Runtime")
	}
}

func TestRetiredSuccessorProjectionDeclinesParentOnlyAfterCurrentModeCheck(t *testing.T) {
	t.Parallel()
	fixture, task := newParentOnlyTaskFixture(t, "")
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	head.ControllerGeneration = 3
	head.RootTaskRef = &task
	head.MetadataLineageRef = &EvidenceObjectRef{}
	head.IntegrationTip = controllerGitOutput(t, fixture.repo, "rev-parse", "HEAD")
	head.ObservedRemoteTip = head.IntegrationTip
	op, selected, err := fixture.store.projectQuiescentParentOnlyMaterialization(head)
	if err != nil || selected || op.Operation == PublicationOperationParentOnlyMaterialize {
		t.Fatalf("ordinary retired successor was blocked by parent-only authority: operation=%#v selected=%t err=%v", op, selected, err)
	}
	next, selected, err := fixture.store.projectCompletionSuccessor(head)
	if err != nil || !selected || next.Operation != PublicationOperationSuccessorStart {
		t.Fatalf("ordinary retired successor did not start: %#v selected=%t err=%v", next, selected, err)
	}
	projected, err := fixture.store.projectQuiescentCompletion(head)
	if err != nil || projected.Operation != PublicationOperationSuccessorStart {
		t.Fatalf("canonical quiescent projection did not advance to ordinary successor: %#v err=%v", projected, err)
	}
	runControllerGit(t, fixture.repo, "rm", "-q", "IMPLEMENTATION_PLAN.local.md")
	runControllerGit(t, fixture.repo, "commit", "-q", "-m", "remove committed task authority")
	if _, selected, err = fixture.store.projectQuiescentParentOnlyMaterialization(head); err == nil || selected {
		t.Fatalf("unresolvable current authority did not fail closed: selected=%t err=%v", selected, err)
	}
	if operation, err := fixture.store.projectQuiescentCompletion(head); err == nil {
		t.Fatalf("canonical successor projection accepted an unresolvable authority: %#v", operation)
	}
}
