package controller

import "testing"

func TestCleanupUsesSealedProjectMetadata(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	base := f.source.Attempt.ExecutionBaseOID
	advanced := amendHeldTaskMetadata(t, f.store, policy, "\nparent Metadata advancement\n")
	authority, err := ResolveCommittedTaskAuthorityAt(f.store.identity.PrimaryRoot, advanced)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.writeProjectSnapshot(authority.Snapshot); err != nil {
		t.Fatal(err)
	}
	trees, err := CaptureExecutionTrees(f.source.Workspace.Root, authority.Snapshot.HeadOID)
	if err != nil {
		t.Fatal(err)
	}
	attempt := f.source.Attempt
	attempt.AttemptState = AttemptStateAccepted
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	seal := AttemptSeal{AttemptID: attempt.AttemptID, ExecutionBaseOID: base, SourceProjectSnapshotID: authority.ProjectSnapshotID, CurrentIndexTree: trees.IndexTree, CurrentWorktreeTree: trees.WorktreeTree}
	if err := f.store.verifySealedCleanupContent(f.source.Workspace, seal); err != nil {
		t.Fatalf("unchanged sealed source was rejected after Metadata advancement: %v", err)
	}
	missing := seal
	missing.SourceProjectSnapshotID = "missing-project"
	if err := f.store.verifySealedCleanupContent(f.source.Workspace, missing); err == nil {
		t.Fatal("missing sealed Project authority was admitted")
	}
	writeSuspensionTestFile(t, f.source.Workspace.Root, "unsealed-source.txt", "unsealed source\n")
	if err := f.store.verifySealedCleanupContent(f.source.Workspace, seal); err == nil {
		t.Fatal("source mutation after sealing was admitted")
	}
}
