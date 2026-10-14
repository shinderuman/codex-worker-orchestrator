package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func reenteredParentSourceFixture(t *testing.T) (findingAcceptanceFixture, Admission) {
	t.Helper()
	f, _, head, candidate := acceptedAmendmentLane(t)
	reentered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	return f, *live.Admission
}

func TestParentSourceImportPreservesAttemptAndEvidenceAcrossRecovery(t *testing.T) {
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "result.txt", "revised task result\n")
	writeSuspensionTestFile(t, repo, "parent-review-fix.txt", "new reviewed source\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{\"retained\":true}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportParentSource(source, "wrong-snapshot", before); err == nil {
		t.Fatal("stale source snapshot admitted")
	}
	stale := source
	stale.Head.ControllerGeneration--
	if _, err := f.store.ImportParentSource(stale, snapshot.ID, before); err == nil {
		t.Fatal("stale generation admitted")
	}
	result, err := f.store.ImportParentSource(source, snapshot.ID, before)
	if err != nil {
		t.Fatal(err)
	}
	live := result.Admission
	if live == nil || live.Attempt.AttemptID != source.Attempt.AttemptID || live.Workspace != source.Workspace || live.Lease.LeaseID == source.Lease.LeaseID {
		t.Fatal("source import replaced Task execution instead of its lease")
	}
	if live.Snapshot.Head != source.Snapshot.Head || live.Snapshot.IndexDigest != source.Snapshot.IndexDigest || live.Snapshot.RefDigest != source.Snapshot.RefDigest {
		t.Fatal("source import changed protected Git authority")
	}
	got, err := os.ReadFile(filepath.Join(live.Workspace.Root, "parent-review-fix.txt"))
	if err != nil || string(got) != "new reviewed source\n" {
		t.Fatal("source import lost implementation")
	}
	got, err = os.ReadFile(filepath.Join(live.Workspace.Root, "result.txt"))
	if err != nil || string(got) != "revised task result\n" {
		t.Fatal("existing implementation was not updated")
	}
	if _, err := f.store.RecoverExecutionOperation(result.TransitionID); err != nil {
		t.Fatal(err)
	}
	data, err := f.store.ReentryReviewRepairEvidence(result.TransitionID)
	if err != nil || string(data) != "{\"retained\":true}" {
		t.Fatalf("retained runtime evidence: %s %v", data, err)
	}
	if result.Head.EvidenceLedgerSequence != source.Head.EvidenceLedgerSequence+1 {
		t.Fatal("repair did not append evidence authority")
	}
	if _, err := f.store.LoadEvidenceHead(*source.Head.EvidenceHeadRef); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
		t.Fatal("old lease remained admitted")
	}
}

func TestParentSourceImportRejectsTaskMetadataChanges(t *testing.T) {
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	path := filepath.Join(repo, source.Attempt.SemanticTaskRef.TaskPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\nforeign authority\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportParentSource(source, snapshot.ID, before); err == nil {
		t.Fatal("changed Task contract admitted")
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, source.Head, after)
}

func TestParentSourceImportBindsNonActiveMetadataWithoutChangingTaskOrIndex(t *testing.T) {
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "IMPLEMENTATION_RULES.md", "parent-managed metadata revision\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.ImportParentSource(source, snapshot.ID, before)
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.ProjectSnapshotID == source.Head.ProjectSnapshotID || !result.Admission.Attempt.SemanticTaskRef.Equal(source.Attempt.SemanticTaskRef) || result.Admission.Snapshot.IndexDigest != source.Snapshot.IndexDigest {
		t.Fatal("parent metadata revision lost Project or execution authority")
	}
	got, err := os.ReadFile(filepath.Join(source.Workspace.Root, "IMPLEMENTATION_RULES.md"))
	if err != nil || string(got) != "parent-managed metadata revision\n" {
		t.Fatal("metadata revision was not materialized")
	}
}

func TestParentSourceImportRecoversOnlyArchivedBeforeOrTargetBytes(t *testing.T) {
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "result.txt", "updated result\n")
	writeSuspensionTestFile(t, repo, "a-added.txt", "added source\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.store.planParentSourceImport(source, snapshot.ID, before)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.prepareExecutionOperation(&op, source.Head); err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.store.PendingExecutionRecoveryTask(pending)
	if err != nil || !task.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatalf("pending recovery task: %#v %v", task, err)
	}
	foreign := pending
	foreign.LiveAttemptID = "foreign-attempt"
	if _, err := f.store.PendingExecutionRecoveryTask(foreign); err == nil {
		t.Fatal("foreign recovery authority admitted")
	}
	writeSuspensionTestFile(t, source.Workspace.Root, "a-added.txt", "added source\n")
	writeSuspensionTestFile(t, source.Workspace.Root, "result.txt", "foreign mutation\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("foreign partial state admitted")
	}
	writeSuspensionTestFile(t, source.Workspace.Root, "result.txt", "task result\n")
	result, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Admission.Attempt.AttemptID != source.Attempt.AttemptID {
		t.Fatal("recovery replaced attempt")
	}
	got, err := os.ReadFile(filepath.Join(source.Workspace.Root, "result.txt"))
	if err != nil || string(got) != "updated result\n" {
		t.Fatal("recovery did not apply archived source")
	}
}
