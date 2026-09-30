package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionAuthorityTransitionPreservesRootAndRevokesSourceLease(t *testing.T) {
	repo, _ := newControllerLinkedWorktree(t)
	childPath := "IMPLEMENTATION_TASKS/child.md"
	if err := os.WriteFile(filepath.Join(repo, childPath), []byte("# child\n\n## Contract\n\nchild execution\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/child.md`\n"
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "add child task")

	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.BootstrapExecution(authority.Task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var child SemanticTaskRef
	for _, task := range project.Tasks {
		if task.TaskPath == childPath {
			child = task
			break
		}
	}
	if child.Empty() {
		t.Fatal("child task missing from committed project snapshot")
	}

	targetAttempt, targetLease, err := newExecutionRecords(
		child,
		source.Attempt.RootTaskRef,
		workspace,
		snapshot,
		source.Head.ControllerGeneration+3,
		"blocker-execution",
	)
	if err != nil {
		t.Fatal(err)
	}
	targetAttempt.AttemptState = AttemptStatePrepared
	targetAttempt.PredecessorAttemptID = source.Attempt.AttemptID
	if err := store.writeAttempt(targetAttempt); err != nil {
		t.Fatal(err)
	}
	if err := store.writeLease(targetLease); err != nil {
		t.Fatal(err)
	}
	targetAuthority := TransitionAuthority{
		ProjectSnapshotID: project.SnapshotID,
		RootTaskRef:       source.Attempt.RootTaskRef,
		ExecutionTaskRef:  child,
		EpisodeID:         source.Head.ActiveEpisodeID,
		EpisodeRevision:   source.Head.ActiveEpisodeRevision,
		AttemptID:         targetAttempt.AttemptID,
		LeaseID:           targetLease.LeaseID,
		WorkspaceID:       workspace.ID,
		WorkspaceSnapshot: snapshot,
	}
	record, err := store.BeginAuthorityTransition(TransitionIntent{
		Kind:               "execution-authority:blocker-execution",
		ExpectedGeneration: source.Head.ControllerGeneration,
		Source:             source,
		Target:             targetAuthority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !record.SourceRootTaskRef.Equal(authority.Task) || !record.TargetRootTaskRef.Equal(authority.Task) {
		t.Fatalf("root authority changed during execution transition: %#v", record)
	}
	if !record.SourceExecutionTaskRef.Equal(authority.Task) || !record.TargetExecutionTaskRef.Equal(child) {
		t.Fatalf("execution authority not machine-bound: %#v", record)
	}
	if record.SourceAttemptID != source.Attempt.AttemptID || record.SourceLeaseID != source.Lease.LeaseID ||
		record.TargetAttemptID != targetAttempt.AttemptID || record.TargetLeaseID != targetLease.LeaseID {
		t.Fatalf("attempt/lease transition authority is incomplete: %#v", record)
	}
	if record.SourceWorkspaceID != workspace.ID || record.TargetWorkspaceID != workspace.ID ||
		record.ProjectSnapshotOld != project.SnapshotID || record.ProjectSnapshotNew != project.SnapshotID {
		t.Fatalf("workspace/project transition authority is incomplete: %#v", record)
	}

	lock, err := store.acquireMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.markTransitionApplied(record, nil); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	committed, err := store.commitAuthorityTransitionLocked(record, nil, false, func(next *RepositoryControllerHead) error {
		root := record.TargetRootTaskRef
		execution := record.TargetExecutionTaskRef
		next.ProjectSnapshotID = record.ProjectSnapshotNew
		next.RootTaskRef = &root
		next.ExecutionTaskRef = &execution
		next.LiveAttemptID = record.TargetAttemptID
		next.LiveLeaseID = record.TargetLeaseID
		return nil
	})
	if err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if committed.ControllerGeneration != record.CommittedGeneration || committed.PendingTransitionID != record.TransitionID {
		_ = lock.Close()
		t.Fatalf("committed transition authority = %#v", committed)
	}
	finalHead, err := store.finalizeAuthorityTransitionLocked(record)
	closeErr := lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	targetAttempt.AttemptState = AttemptStateLive
	if err := store.writeAttempt(targetAttempt); err != nil {
		t.Fatal(err)
	}
	target := Admission{Head: finalHead, Attempt: targetAttempt, Lease: targetLease, Workspace: workspace, Snapshot: snapshot}
	if target.Head.RootTaskRef == nil || !target.Head.RootTaskRef.Equal(authority.Task) {
		t.Fatalf("finalized transition lost root authority: %#v", target.Head)
	}
	if target.Head.ExecutionTaskRef == nil || !target.Head.ExecutionTaskRef.Equal(child) {
		t.Fatalf("finalized transition did not select child execution authority: %#v", target.Head)
	}
	if target.Lease.LeaseID != targetLease.LeaseID || target.Attempt.AttemptID != targetAttempt.AttemptID {
		t.Fatalf("finalized transition selected unexpected attempt/lease: %#v", target)
	}
	if _, err := store.AdmitMutation(source.Lease.SemanticTaskRef, source.Workspace, source.Snapshot); err == nil {
		t.Fatal("source lease remained admissible after execution authority changed")
	}
	if _, err := store.AdmitMutation(child, workspace, snapshot); err != nil {
		t.Fatalf("target execution authority was not admissible: %v", err)
	}
}
